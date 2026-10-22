package traces

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func filterThenSelect(sel map[string]interface{}) []map[string]interface{} {
	return []map[string]interface{}{
		{"type": "filter", "query": map[string]interface{}{"$neq": []interface{}{"ServiceName", ""}}},
		sel,
	}
}

// The engine applies a final select's own limit only when it is positive and
// ignores the request limit, so 0, -1 or a missing limit returns every span.
func TestSanitizeTraceJSONQuery_FinalSelectLimitBounds(t *testing.T) {
	bad := map[string]map[string]interface{}{
		"limit 0":      {"type": "select", "order": []interface{}{"Duration"}, "limit": 0.0},
		"limit -1":     {"type": "select", "order": []interface{}{"Duration"}, "limit": -1.0},
		"limit 1001":   {"type": "select", "order": []interface{}{"Duration"}, "limit": 1001.0},
		"limit str":    {"type": "select", "order": []interface{}{"Duration"}, "limit": "10"},
		"limit frac":   {"type": "select", "order": []interface{}{"Duration"}, "limit": 10.5},
		"offset -1":    {"type": "select", "order": []interface{}{"Duration"}, "limit": 10.0, "offset": -1.0},
		"offset 10001": {"type": "select", "order": []interface{}{"Duration"}, "limit": 10.0, "offset": 10001.0},
	}
	for name, sel := range bad {
		t.Run(name, func(t *testing.T) {
			err := SanitizeTraceJSONQuery(filterThenSelect(sel))
			if err == nil {
				t.Fatalf("expected rejection for %v", sel)
			}
			if !strings.Contains(err.Error(), "tracejson_query[1]") {
				t.Fatalf("error %q should name tracejson_query[1]", err)
			}
		})
	}
	good := map[string]map[string]interface{}{
		"limit 10":         {"type": "select", "order": []interface{}{"Duration"}, "limit": 10.0},
		"limit int":        {"type": "select", "order": []interface{}{"Duration"}, "limit": 1000},
		"offset":           {"type": "select", "order": []interface{}{"Duration"}, "limit": 10.0, "offset": 20.0},
		"labels, no limit": {"type": "select", "labels": map[string]interface{}{"ServiceName": "svc"}},
	}
	for name, sel := range good {
		t.Run(name, func(t *testing.T) {
			if err := SanitizeTraceJSONQuery(filterThenSelect(sel)); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

type selectBodyRecorder struct {
	mu     sync.Mutex
	bodies []map[string]interface{}
}

func (r *selectBodyRecorder) handler(w http.ResponseWriter, req *http.Request) {
	raw, _ := io.ReadAll(req.Body)
	var body map[string]interface{}
	_ = json.Unmarshal(raw, &body)
	r.mu.Lock()
	r.bodies = append(r.bodies, body)
	r.mu.Unlock()
	_, _ = w.Write([]byte(traceAPIResponse(1)))
}

func lastStage(t *testing.T, body map[string]interface{}) map[string]interface{} {
	t.Helper()
	stages, _ := body["pipeline"].([]interface{})
	if len(stages) == 0 {
		t.Fatalf("request body has no pipeline: %v", body)
	}
	stage, _ := stages[len(stages)-1].(map[string]interface{})
	return stage
}

func TestGetTracesHandlerRunsSelectAsOneBoundedRequest(t *testing.T) {
	cases := []struct {
		name      string
		sel       map[string]interface{}
		argLimit  int
		wantLimit float64
	}{
		{"missing limit takes the request limit", map[string]interface{}{"type": "select", "labels": map[string]interface{}{"ServiceName": "svc"}}, 25, 25},
		{"missing limit is capped at 1000", map[string]interface{}{"type": "select", "order": []interface{}{"Duration"}}, 5000, 1000},
		{"explicit limit is kept", map[string]interface{}{"type": "select", "order": []interface{}{"Duration"}, "limit": 10.0}, 50, 10},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := &selectBodyRecorder{}
			server := httptest.NewServer(http.HandlerFunc(rec.handler))
			defer server.Close()

			handler := NewGetTracesHandler(server.Client(), testChunkTracesConfig(server.URL))
			_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetTracesArgs{
				TracejsonQuery: filterThenSelect(tc.sel),
				// 7 days: a plain filter would be split into many requests, each
				// returning its own top-N.
				StartTimeISO: "2026-01-01T00:00:00Z",
				EndTimeISO:   "2026-01-08T00:00:00Z",
				Limit:        tc.argLimit,
			})
			if err != nil {
				t.Fatalf("handler returned error: %v", err)
			}
			if len(rec.bodies) != 1 {
				t.Fatalf("expected 1 request for a select pipeline, got %d", len(rec.bodies))
			}
			if got := lastStage(t, rec.bodies[0])["limit"]; got != tc.wantLimit {
				t.Fatalf("final select limit = %v, want %v", got, tc.wantLimit)
			}
		})
	}
}
