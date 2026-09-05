package alerting

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// countUpstream returns an httptest server that increments an atomic counter on
// every request and replies with an empty (but valid) alert_rules payload.
func countUpstream(hits *int64) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(hits, 1)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"timestamp":0,"window":1,"alert_rules":[]}`))
	}))
}

func resultIsError(t *testing.T, result *mcp.CallToolResult, wantSubstr string) {
	t.Helper()
	if result == nil || !result.IsError {
		t.Fatalf("expected IsError=true result, got %+v", result)
	}
	if len(result.Content) == 0 {
		t.Fatalf("expected content in error result")
	}
	text, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result.Content[0])
	}
	if !strings.Contains(text.Text, wantSubstr) {
		t.Fatalf("expected error message containing %q, got %q", wantSubstr, text.Text)
	}
}

// TestAlertRuleStateHandler_RejectsSubtractionOverflow verifies the reachable
// bypass from the bug report: when start_time/end_time sit near -2^63 / +2^63
// (both float64-representable and therefore delivered intact by the SDK
// transport), end_time - start_time overflows int64 to a small negative number.
// Without the span<0 guard, `points` evaluates to a tiny value (e.g. 1) and the
// 100-sample cap is bypassed; the sampling loop then runs ~10^10 iterations,
// each issuing an authenticated /alerts/monitor call. The fix rejects the
// overflow up front and makes zero upstream calls.
func TestAlertRuleStateHandler_RejectsSubtractionOverflow(t *testing.T) {
	var hits int64
	upstream := countUpstream(&hits)
	defer upstream.Close()

	handler := NewAlertRuleStateHandler(upstream.Client(), newAlertRuleStateTestConfig(upstream.URL))

	// Int64 values the transport delivers after the float64 round-trip
	// (bug report §1: the raw 9223372036854773760 round-trips to the nearest
	// representable double, 9223372036854774000).
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, AlertRuleStateRequest{
		StartTime: -9223372036854774000,
		EndTime:   9223372036854774000,
		Step:      1000000000,
	})
	if err != nil {
		t.Fatalf("expected IsError result, got transport error: %v", err)
	}
	resultIsError(t, result, "overflows")
	if got := atomic.LoadInt64(&hits); got != 0 {
		t.Fatalf("expected zero upstream calls for rejected overflow input, got %d", got)
	}
}

// TestAlertRuleStateHandler_RejectsPlusOneOverflow guards the points
// computation span/step + 1 itself: with span = MaxInt64 and step = 1 the +1
// wraps to MinInt64 (< 0). The points<0 check rejects this before any request.
//
// Note: this exact input is rejected at the transport boundary (the float64
// round-trip of MaxInt64 overflows int64), so this is a direct-handler test
// covering the arithmetic guard in isolation.
func TestAlertRuleStateHandler_RejectsPlusOneOverflow(t *testing.T) {
	var hits int64
	upstream := countUpstream(&hits)
	defer upstream.Close()

	handler := NewAlertRuleStateHandler(upstream.Client(), newAlertRuleStateTestConfig(upstream.URL))

	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, AlertRuleStateRequest{
		StartTime: 0,
		EndTime:   math.MaxInt64,
		Step:      1,
	})
	if err != nil {
		t.Fatalf("expected IsError result, got transport error: %v", err)
	}
	resultIsError(t, result, "too many points")
	if got := atomic.LoadInt64(&hits); got != 0 {
		t.Fatalf("expected zero upstream calls for rejected +1-overflow input, got %d", got)
	}
}

// TestAlertRuleStateHandler_LoopBoundedByCap drives the handler with a range
// yielding exactly the maximum sample count and asserts the sampling loop
// issues precisely that many upstream calls — proving the loop is index-bounded
// by the validated `points` value and runs exactly `points` times, never more.
func TestAlertRuleStateHandler_LoopBoundedByCap(t *testing.T) {
	var hits int64
	upstream := countUpstream(&hits)
	defer upstream.Close()

	handler := NewAlertRuleStateHandler(upstream.Client(), newAlertRuleStateTestConfig(upstream.URL))

	// span=99, step=1 -> points = 99/1 + 1 = 100 (exactly the cap).
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, AlertRuleStateRequest{
		StartTime: 0,
		EndTime:   99,
		Step:      1,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected IsError at the cap boundary: %+v", result.Content)
	}
	if got, want := atomic.LoadInt64(&hits), int64(alertRuleStateMaxPoints); got != want {
		t.Fatalf("expected exactly %d upstream calls at the cap boundary, got %d", want, got)
	}
}

// TestAlertRuleStateHandler_TransportBypassBlocked reproduces the bug report's
// end-to-end scenario through the real MCP transport (server + in-memory
// transport + client session, exercising the full applySchema -> remarshal ->
// typed-unmarshal decode pipeline) and asserts the overflow input is now
// blocked at the handler and never exceeds the documented 100-sample cap.
// A hard timeout prevents the suite from hanging if the fix regresses.
func TestAlertRuleStateHandler_TransportBypassBlocked(t *testing.T) {
	var hits int64
	upstream := countUpstream(&hits)
	defer upstream.Close()

	server := mcp.NewServer(&mcp.Implementation{Name: "s", Version: "v"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "get_alert_rule_state", Description: "x"},
		NewAlertRuleStateHandler(upstream.Client(), newAlertRuleStateTestConfig(upstream.URL)))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	t1, t2 := mcp.NewInMemoryTransports()
	if _, err := server.Connect(ctx, t1, nil); err != nil {
		t.Fatal(err)
	}
	session, err := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "v"}, nil).Connect(ctx, t2, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	// The exact raw JSON from the bug report: start/end near ±2^63 that survive
	// the SDK's float64 decode round-trip (verified reachable in bug report §1).
	res, _ := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "get_alert_rule_state",
		Arguments: json.RawMessage(`{"start_time":-9223372036854773760,"end_time":9223372036854773760,"step":1000000000}`),
	})

	if got := atomic.LoadInt64(&hits); got > alertRuleStateMaxPoints {
		t.Fatalf("cap bypassed through transport: handler made %d upstream calls (max %d)", got, alertRuleStateMaxPoints)
	}
	if res == nil || !res.IsError {
		t.Fatalf("expected IsError result for overflow input, got %+v", res)
	}
}
