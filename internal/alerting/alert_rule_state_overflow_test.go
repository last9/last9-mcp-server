package alerting

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Overflow *rejection* cases live in TestAlertRuleStateHandler_ValidationErrors.
// This file keeps the two overflow regression tests that need an upstream
// call counter or the real MCP transport.

// countUpstream returns an httptest server that increments an atomic counter on
// every request and replies with an empty (but valid) alert_rules payload.
func countUpstream(hits *int64) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(hits, 1)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"timestamp":0,"window":1,"alert_rules":[]}`))
	}))
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
		StartTime: ts(1718000000),
		EndTime:   ts(1718000099),
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

// TestAlertRuleStateHandler_TransportBypassBlocked reproduces the original
// overflow bypass end-to-end through the real MCP transport (server + in-memory
// transport + client session, exercising the full applySchema -> remarshal ->
// typed-unmarshal decode pipeline, with the served input schema) and asserts
// the handler's timestamp guard rejects the overflow input before any upstream call.
// A hard timeout prevents the suite from hanging if the fix regresses.
func TestAlertRuleStateHandler_TransportBypassBlocked(t *testing.T) {
	var hits int64
	upstream := countUpstream(&hits)
	defer upstream.Close()

	server := mcp.NewServer(&mcp.Implementation{Name: "s", Version: "v"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "get_alert_rule_state", Description: "x", InputSchema: AlertRuleStateInputSchema()},
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

	// Raw JSON with start/end near ±2^63 that survive the SDK's float64
	// decode round-trip.
	res, _ := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "get_alert_rule_state",
		Arguments: json.RawMessage(`{"start_time":-9223372036854773760,"end_time":9223372036854773760,"step":1000000000}`),
	})

	if got := atomic.LoadInt64(&hits); got != 0 {
		t.Fatalf("overflow input reached upstream: %d calls, want 0", got)
	}
	if res == nil || !res.IsError {
		t.Fatalf("expected IsError result for overflow input, got %+v", res)
	}
	// The handler's guard, not schema validation, must be what rejects it.
	if text := res.Content[0].(*mcp.TextContent).Text; !strings.Contains(text, "before 2000-01-01") {
		t.Fatalf("want the handler's pre-2000 rejection, got %q", text)
	}
}
