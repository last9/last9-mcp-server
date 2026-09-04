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

// hardcodedSelector matches any literal PromQL duration selector (e.g.
// [5m], [1h]) — a regression to a different hardcoded duration would slip
// past a check for "[5m]" alone.
var hardcodedSelector = regexp.MustCompile(`\[\d+[smhdwy]\]`)

// perfDetailsCapture records the PromQL and resolution of every sub-query the
// handler sends, split by endpoint.
type perfDetailsCapture struct {
	mu       sync.Mutex
	rangeQ   []string
	instantQ []string
	rangeMDP []int64
}

func (c *perfDetailsCapture) snapshot() ([]string, []string, []int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.rangeQ...),
		append([]string(nil), c.instantQ...),
		append([]int64(nil), c.rangeMDP...)
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
		}
		_ = json.Unmarshal(body, &payload)

		capture.mu.Lock()
		switch r.URL.Path {
		case constants.EndpointPromQuery:
			capture.rangeQ = append(capture.rangeQ, payload.Query)
			capture.rangeMDP = append(capture.rangeMDP, payload.MaxDataPoints)
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

// ENG-1823: step and selector must move together. Every range sub-query asks
// the server to size its selector from the step it chose.
func TestPerfDetails_AllRangeQueriesUseRateInterval(t *testing.T) {
	rangeQ, _, _ := runPerfDetailsCapture(t).snapshot()

	if len(rangeQ) != 6 {
		t.Fatalf("expected 6 range sub-queries, got %d", len(rangeQ))
	}
	for _, q := range rangeQ {
		if !strings.Contains(q, "$__rate_interval") {
			t.Errorf("range query has no $__rate_interval; a widened step would outgrow its selector:\n%s", q)
		}
		if m := hardcodedSelector.FindString(q); m != "" {
			t.Errorf("range query carries a hardcoded selector %s; it must be sized from the step:\n%s", m, q)
		}
	}
}

// Apdex is a bare gauge: without a rollup wrapper a widened step reads almost
// nothing, regardless of what the selector says.
func TestPerfDetails_ApdexIsWrappedInRollup(t *testing.T) {
	rangeQ, _, _ := runPerfDetailsCapture(t).snapshot()

	var apdex string
	for _, q := range rangeQ {
		if strings.Contains(q, "trace_service_apdex_score") {
			apdex = q
			break
		}
	}
	if apdex == "" {
		t.Fatal("no apdex sub-query was sent")
	}
	if !strings.Contains(apdex, "avg_over_time(") {
		t.Errorf("apdex is still a bare selector; a widened step drops most of its points:\n%s", apdex)
	}
	if !strings.Contains(apdex, "$__rate_interval") {
		t.Errorf("apdex rollup window is not sized from the step:\n%s", apdex)
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
