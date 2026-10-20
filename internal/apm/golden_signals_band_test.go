package apm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// newPromRangeStub answers range queries by exact query text with one point
// per window end. It records every request body it saw.
func newPromRangeStub(t *testing.T, ends []int64, answers map[string][]promStubSample) (*httptest.Server, func() []map[string]any) {
	t.Helper()
	var bodies []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/prom_query") {
			t.Errorf("unexpected request %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("decode prom request: %v", err)
		}
		bodies = append(bodies, body)
		out := make([]map[string]any, 0)
		for _, s := range answers[body["query"].(string)] {
			values := make([][]any, 0, len(ends))
			for i, end := range ends {
				v, _ := strconv.ParseFloat(s.value, 64)
				values = append(values, []any{float64(end), strconv.FormatFloat(v*float64(i+1), 'f', -1, 64)})
			}
			out = append(out, map[string]any{"metric": s.labels, "values": values})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	}))
	return server, func() []map[string]any { return bodies }
}

// One range query per signal, stepped by the window length, yields every
// window's golden signals; points after End are not windows before it.
func TestQueryGoldenSignalsWindowsSlicesOneRangeQueryPerSignal(t *testing.T) {
	end := time.Date(2026, 10, 16, 10, 0, 0, 0, time.UTC)
	q := GoldenSignalsWindowsQuery{Services: []string{"checkout"}, Env: "prod", End: end, WindowMinutes: 30, Count: 2}
	matcher := goldenSignalsMatcher(q.Services, q.Env)
	answers := map[string][]promStubSample{
		serviceSummaryCountQuery(matcher, 30, ""): {
			{map[string]string{"service_name": "checkout", "env": "prod"}, "600"},
		},
		serviceSummaryCountQuery(matcher, 30, `http_status_code=~"5.*"`): {
			{map[string]string{"service_name": "checkout", "env": "prod"}, "6"},
		},
		goldenLatencyQuery(matcher, 30): {
			{map[string]string{"service_name": "checkout", "env": "prod", "quantile": "p95"}, "100"},
		},
	}
	window := int64(30 * 60)
	ends := []int64{end.Unix() - window, end.Unix(), end.Unix() + window}
	server, bodies := newPromRangeStub(t, ends, answers)
	defer server.Close()

	got, err := QueryGoldenSignalsWindows(context.Background(), server.Client(), testSummaryConfig(server.URL), q)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("windows = %d, want 2 (the point after End dropped): %+v", len(got), got)
	}
	for i, w := range got {
		n := float64(i + 1)
		if !w.End.Equal(time.Unix(ends[i], 0)) {
			t.Fatalf("window %d ends %s, want %s", i, w.End, time.Unix(ends[i], 0))
		}
		checkout := w.Signals["checkout"]
		if checkout.RequestCount != 600*n || checkout.ThroughputRPM != 20*n || checkout.ErrorRatePct == nil || *checkout.ErrorRatePct != 1 {
			t.Fatalf("window %d checkout counts = %+v", i, checkout)
		}
		if p95 := checkout.LatencyByEnv["prod"].P95Ms; p95 == nil || *p95 != 100*n {
			t.Fatalf("window %d p95 = %v, want %v", i, p95, 100*n)
		}
	}

	sent := bodies()
	if len(sent) != len(serviceSummaryQueried)+1 {
		t.Fatalf("sent %d queries, want one per count class plus latency", len(sent))
	}
	for _, b := range sent {
		if b["timestamp"] != float64(end.Unix()) || b["window"] != float64(window) || b["step"] != float64(window) {
			t.Fatalf("range request = %v, want timestamp %d, window %d, step %d", b, end.Unix(), window, window)
		}
	}
}
