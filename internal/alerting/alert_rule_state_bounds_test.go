package alerting

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// hangingUpstream never answers until the test ends, then releases its handlers
// before closing so a regression fails the test instead of hanging the suite.
func hangingUpstream(t *testing.T) *httptest.Server {
	stop := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-stop:
		}
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(stop) })
	return srv
}

func TestAlertRuleStateHandler_ReturnsGuidanceWhenSamplingTimesOut(t *testing.T) {
	restore := alertRuleStateTimeout
	alertRuleStateTimeout = 100 * time.Millisecond
	t.Cleanup(func() { alertRuleStateTimeout = restore })

	upstream := hangingUpstream(t)

	handler := NewAlertRuleStateHandler(upstream.Client(), newAlertRuleStateTestConfig(upstream.URL))
	done := make(chan struct{})
	var result *mcp.CallToolResult
	var err error
	go func() {
		defer close(done)
		result, _, err = handler(context.Background(), &mcp.CallToolRequest{}, AlertRuleStateRequest{StartTime: ts(1718000000), EndTime: ts(1718003540), Step: 60})
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("handler ignored its sampling deadline")
	}

	if err != nil {
		t.Fatalf("expected a tool result, got error: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected IsError, got %+v", result.Content)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	for _, want := range []string{"timed out after 100ms", "0 of 60 samples", "increase step", "narrow start_time/end_time", "alert_group_id", "rule_name"} {
		if !strings.Contains(text, want) {
			t.Errorf("message %q lacks %q", text, want)
		}
	}
}

func TestAlertRuleStateHandler_CallerCancellationIsNotATimeout(t *testing.T) {
	upstream := hangingUpstream(t)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	handler := NewAlertRuleStateHandler(upstream.Client(), newAlertRuleStateTestConfig(upstream.URL))
	_, _, err := handler(ctx, &mcp.CallToolRequest{}, AlertRuleStateRequest{StartTime: ts(1718000000), EndTime: ts(1718000060), Step: 60})
	if err == nil {
		t.Fatal("expected the caller's cancellation as an error")
	}
}

func TestAlertRuleStateHandler_SamplesConcurrentlyUpToLimit(t *testing.T) {
	var mu sync.Mutex
	inFlight, maxInFlight := 0, 0
	full := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		inFlight++
		maxInFlight = max(maxInFlight, inFlight)
		if inFlight == alertRuleStateConcurrency {
			close(full)
		}
		mu.Unlock()
		// Hold every request until the limit is reached, so a sequential loop
		// shows up as max in-flight 1 rather than passing by timing luck.
		select {
		case <-full:
		case <-time.After(2 * time.Second):
		}
		mu.Lock()
		inFlight--
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"alert_rules":[{"rule_id":"r","state":"firing"}]}`))
	}))
	defer upstream.Close()

	handler := NewAlertRuleStateHandler(upstream.Client(), newAlertRuleStateTestConfig(upstream.URL))
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, AlertRuleStateRequest{StartTime: ts(1718000000), EndTime: ts(1718000000 + 19*60), Step: 60})
	if err != nil || result.IsError {
		t.Fatalf("unexpected failure: err=%v result=%+v", err, result)
	}
	if maxInFlight != alertRuleStateConcurrency {
		t.Errorf("max concurrent upstream calls = %d, want %d", maxInFlight, alertRuleStateConcurrency)
	}
	series := decodeAlertRuleStateResult(t, result)["r"]
	if len(series) != 20 {
		t.Fatalf("got %d datapoints, want 20", len(series))
	}
	for i, dp := range series {
		if want := int64(1718000000 + i*60); dp.Timestamp != want || dp.IsFiring != 1 {
			t.Errorf("datapoint %d = %+v, want firing at %d", i, dp, want)
		}
	}
}

func TestAlertRuleStateHandler_CapsWindowAtMonitorMaximum(t *testing.T) {
	cases := []struct {
		step int64
		want string
	}{
		{MaxAlertsMonitorWindowSeconds - 1, "3599"},
		{MaxAlertsMonitorWindowSeconds, "3600"},
		{MaxAlertsMonitorWindowSeconds + 1, "3600"},
		{86400, "3600"},
	}
	for _, tc := range cases {
		var mu sync.Mutex
		windows := map[string]bool{}
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			windows[r.URL.Query().Get("window")] = true
			mu.Unlock()
			w.Write([]byte(`{"alert_rules":[]}`))
		}))
		handler := NewAlertRuleStateHandler(upstream.Client(), newAlertRuleStateTestConfig(upstream.URL))
		result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, AlertRuleStateRequest{StartTime: ts(1718000000), EndTime: ts(1718000000 + 2*tc.step), Step: tc.step})
		upstream.Close()
		if err != nil || result.IsError {
			t.Fatalf("step %d: unexpected failure: err=%v result=%+v", tc.step, err, result)
		}
		if len(windows) != 1 || !windows[tc.want] {
			t.Errorf("step %d: windows sent = %v, want only %s", tc.step, windows, tc.want)
		}
	}
}

// Completed samples must keep only rule id and state: annotations and labels never
// reach the output, and 100 samples of them would otherwise stay on the heap.
func TestAlertRuleStateHandler_DropsUnusedMetadataFromCompletedSamples(t *testing.T) {
	payload := `{"alert_rules":[{"rule_id":"r","state":"firing","alerts":[{"annotations":{"details":"` + strings.Repeat("x", 1<<20) + `"}}]}]}`
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	var hits atomic.Int64
	var retained atomic.Uint64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) == alertRuleStateMaxPoints {
			runtime.GC()
			var current runtime.MemStats
			runtime.ReadMemStats(&current)
			if current.HeapAlloc > before.HeapAlloc {
				retained.Store(current.HeapAlloc - before.HeapAlloc)
			}
		}
		w.Write([]byte(payload))
	}))
	defer upstream.Close()

	handler := NewAlertRuleStateHandler(upstream.Client(), newAlertRuleStateTestConfig(upstream.URL))
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, AlertRuleStateRequest{StartTime: ts(1718000000), EndTime: ts(1718000099), Step: 1})
	if err != nil || result.IsError {
		t.Fatalf("err=%v result=%+v", err, result)
	}
	if got := retained.Load(); got > 20<<20 {
		t.Errorf("heap grew %.1f MiB by the last sample; completed samples retain unused metadata", float64(got)/(1<<20))
	}
}

func TestAlertRuleStateHandler_RecommendsSmallestAcceptedStep(t *testing.T) {
	srv, _ := sampledTimestamps(t)
	handler := NewAlertRuleStateHandler(srv.Client(), newAlertRuleStateTestConfig(srv.URL))
	call := func(span, step int64) *mcp.CallToolResult {
		t.Helper()
		result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, AlertRuleStateRequest{StartTime: ts(1718000000), EndTime: ts(1718000000 + span), Step: step})
		if err != nil {
			t.Fatalf("span %d step %d: %v", span, step, err)
		}
		return result
	}
	cases := []struct{ span, rejectedStep, want int64 }{
		{12000, 120, 121},
		{6000, 60, 61},
		{100, 1, 2},
		{9999, 1, 100},
		{123457, 1, 1235},
	}
	for _, tc := range cases {
		result := call(tc.span, tc.rejectedStep)
		text := result.Content[0].(*mcp.TextContent).Text
		if !result.IsError || !strings.Contains(text, fmt.Sprintf("set step to at least %d seconds", tc.want)) {
			t.Errorf("span %d step %d: got %q, want a recommendation of %d", tc.span, tc.rejectedStep, text, tc.want)
		}
		if call(tc.span, tc.want).IsError {
			t.Errorf("span %d: recommended step %d is rejected", tc.span, tc.want)
		}
		if !call(tc.span, tc.want-1).IsError {
			t.Errorf("span %d: step %d is accepted, so %d is not the smallest", tc.span, tc.want-1, tc.want)
		}
	}
}
