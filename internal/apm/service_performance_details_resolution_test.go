package apm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"last9-mcp/internal/constants"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Matches any literal duration selector, incl. compound ([1h30m]) and
// fractional ([1.5h]) — a check for "[5m]" alone would miss those.
var hardcodedSelector = regexp.MustCompile(`\[[\d.]+[a-z]+(\d+[a-z]+)*\]`)

// perfDetailsCapture records the PromQL and resolution of every sub-query the
// handler sends, split by endpoint.
type perfDetailsCapture struct {
	mu        sync.Mutex
	rangeQ    []string
	instantQ  []string
	rangeMDP  []int64
	rangeStep []int64
}

func (c *perfDetailsCapture) snapshot() ([]string, []string, []int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.rangeQ...),
		append([]string(nil), c.instantQ...),
		append([]int64(nil), c.rangeMDP...)
}

// snapshotSteps returns the "step" value captured for every range sub-query,
// in the same order as snapshot's rangeQ.
func (c *perfDetailsCapture) snapshotSteps() []int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]int64(nil), c.rangeStep...)
}

// runPerfDetailsCapture drives the handler over a 10-day (single-chunk) window
// and returns every sub-query it sent.
func runPerfDetailsCapture(t *testing.T) *perfDetailsCapture {
	t.Helper()
	capture := &perfDetailsCapture{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload struct {
			Query         string `json:"query"`
			MaxDataPoints int64  `json:"max_data_points"`
			Step          int64  `json:"step"`
		}
		_ = json.Unmarshal(body, &payload)

		capture.mu.Lock()
		switch r.URL.Path {
		case constants.EndpointPromQuery:
			capture.rangeQ = append(capture.rangeQ, payload.Query)
			capture.rangeMDP = append(capture.rangeMDP, payload.MaxDataPoints)
			capture.rangeStep = append(capture.rangeStep, payload.Step)
		case constants.EndpointPromQueryInstant:
			capture.instantQ = append(capture.instantQ, payload.Query)
		}
		capture.mu.Unlock()

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("[]"))
	}))
	t.Cleanup(server.Close)

	handler := NewServicePerformanceDetailsHandler(server.Client(), perfDetailsTestConfig(server.URL))
	now := time.Now().UTC()
	args := ServicePerformanceDetailsArgs{
		ServiceName:  "svc",
		StartTimeISO: now.Add(-10 * 24 * time.Hour).Format(time.RFC3339),
		EndTimeISO:   now.Format(time.RFC3339),
		Env:          "prod",
	}

	if _, _, err := handler(context.Background(), &mcp.CallToolRequest{}, args); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	return capture
}

// Counting occurrences, not Contains: availability and error_percent carry
// two selectors each, so one regressing could hide behind the other's
// surviving occurrence.
func TestPerfDetails_AllRangeQueriesUseRateInterval(t *testing.T) {
	rangeQ, _, _ := runPerfDetailsCapture(t).snapshot()

	if len(rangeQ) != 6 {
		t.Fatalf("expected 6 range sub-queries, got %d", len(rangeQ))
	}

	// response_times 1, availability 2, throughput 1, error_rate 1,
	// error_percent 2 = 7. Apdex is a bare gauge and carries none: VM sizes a
	// bare selector's lookback from the step, so it needs no selector.
	const wantTotalRateIntervals = 7
	total := 0
	for _, q := range rangeQ {
		total += strings.Count(q, "$__rate_interval")
		if m := hardcodedSelector.FindString(q); m != "" {
			t.Errorf("range query carries a hardcoded selector %s; it must be sized from the step:\n%s", m, q)
		}
	}
	if total != wantTotalRateIntervals {
		t.Errorf("$__rate_interval occurrences across all range queries = %d, want %d", total, wantTotalRateIntervals)
	}
}

// A Step would be absolute, outgrowing a hardcoded selector, and would go
// unnoticed by a max_data_points-only check.
func TestPerfDetails_RangeQueriesSendNoStep(t *testing.T) {
	capture := runPerfDetailsCapture(t)
	rangeQ, _, _ := capture.snapshot()
	steps := capture.snapshotSteps()

	if len(steps) != len(rangeQ) {
		t.Fatalf("captured %d queries but %d step values", len(rangeQ), len(steps))
	}
	for i, step := range steps {
		if step != 0 {
			t.Errorf("range query %d sent step = %d, want 0 (absent):\n%s", i, step, rangeQ[i])
		}
	}
}

// The three top-k sub-queries are instant queries whose selector IS the chunk
// width. $__rate_interval is meaningless there and the server does not
// substitute it on the instant endpoint.
func TestPerfDetails_TopKQueriesKeepChunkWidthSelector(t *testing.T) {
	_, instantQ, _ := runPerfDetailsCapture(t).snapshot()

	if len(instantQ) != 3 {
		t.Fatalf("expected 3 instant sub-queries, got %d", len(instantQ))
	}
	for _, q := range instantQ {
		if strings.Contains(q, "$__rate_interval") {
			t.Errorf("instant query carries $__rate_interval; the instant endpoint does not substitute it:\n%s", q)
		}
	}
}

// 50,401 points per series over 35 days becomes ~200. Safe only because
// every range selector is sized from the resulting step.
func TestPerfDetails_RangeQueriesSendPointBudget(t *testing.T) {
	rangeQ, _, rangeMDP := runPerfDetailsCapture(t).snapshot()

	if len(rangeMDP) != len(rangeQ) {
		t.Fatalf("captured %d queries but %d budgets", len(rangeQ), len(rangeMDP))
	}
	if len(rangeMDP) != 6 {
		t.Fatalf("expected 6 range sub-queries, got %d", len(rangeMDP))
	}
	for i, mdp := range rangeMDP {
		if mdp != perfDetailsMaxDataPoints {
			t.Errorf("range query %d sent max_data_points = %d, want %d:\n%s",
				i, mdp, perfDetailsMaxDataPoints, rangeQ[i])
		}
	}
}

// The instant endpoint has no step, so a budget there is meaningless. Sending
// one would also be the first step toward widening a chunk-width selector.
func TestPerfDetails_InstantQueriesSendNoPointBudget(t *testing.T) {
	var mu sync.Mutex
	instantCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.URL.Path == constants.EndpointPromQueryInstant {
			var raw map[string]any
			_ = json.Unmarshal(body, &raw)
			mu.Lock()
			instantCount++
			mu.Unlock()
			// t.Errorf is goroutine-safe; t.Fatalf is not. Never Fatalf here.
			if _, ok := raw["max_data_points"]; ok {
				t.Errorf("instant sub-query carried max_data_points: %s", body)
			}
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("[]"))
	}))
	defer server.Close()

	handler := NewServicePerformanceDetailsHandler(server.Client(), perfDetailsTestConfig(server.URL))
	now := time.Now().UTC()
	args := ServicePerformanceDetailsArgs{
		ServiceName:  "svc",
		StartTimeISO: now.Add(-10 * 24 * time.Hour).Format(time.RFC3339),
		EndTimeISO:   now.Format(time.RFC3339),
		Env:          "prod",
	}
	if _, _, err := handler(context.Background(), &mcp.CallToolRequest{}, args); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if instantCount != 3 {
		t.Fatalf("expected 3 instant sub-queries, got %d", instantCount)
	}
}
