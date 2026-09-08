package alerting

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Overflow *rejection* cases live in TestAlertRuleStateHandler_ValidationErrors.
// This file keeps the two regression tests for #246 that need an upstream
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

// TestAlertRuleStateHandler_TransportBypassBlocked reproduces the #246
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

	// The exact raw JSON from #246: start/end near ±2^63 that survive the
	// SDK's float64 decode round-trip.
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
