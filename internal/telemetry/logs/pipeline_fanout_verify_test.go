package logs

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func countingLogsServer(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var n atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"status":"error","error":"invalid pipeline"}`)
	}))
	t.Cleanup(server.Close)
	return server, &n
}

func waitForRequests(t *testing.T, n *atomic.Int32, want int32) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if n.Load() >= want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestENG1729_SanitizeAcceptsTraceDomainAndMalformedStages documents that
// sanitizeLogJSONQuery is not fail-closed for the four hosted shapes.
func TestENG1729_SanitizeAcceptsTraceDomainAndMalformedStages(t *testing.T) {
	cases := []struct {
		name   string
		stages []map[string]interface{}
	}{
		{
			name: "SpanKind filter is a traces field",
			stages: []map[string]interface{}{
				{"type": "filter", "query": map[string]interface{}{
					"$and": []interface{}{
						map[string]interface{}{"$eq": []interface{}{"SpanKind", "SPAN_KIND_SERVER"}},
					},
				}},
			},
		},
		{
			name: "parse uses format instead of parser",
			stages: []map[string]interface{}{
				{"type": "parse", "format": "json"},
			},
		},
		{
			name: "window_aggregate uses aggregates+window_minutes",
			stages: []map[string]interface{}{
				{
					"type":           "window_aggregate",
					"aggregates":     []interface{}{map[string]interface{}{"function": map[string]interface{}{"$count": []interface{}{}}, "as": "_count"}},
					"window_minutes": 5,
				},
			},
		},
		{
			name: "unknown stage key is copied through",
			stages: []map[string]interface{}{
				{"type": "filter", "query": map[string]interface{}{"$eq": []interface{}{"ServiceName", "checkout"}}, "bogus": true},
			},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := sanitizeLogJSONQuery(tt.stages); err != nil {
				t.Fatalf("sanitize currently fails closed for %s: %v", tt.name, err)
			}
		})
	}
}

// TestENG1729_SpanKindDiscoveryFansOutTwoUpstreamRequests records the hosted
// fan-out: invalid discovery launches body sampling in parallel with series.
func TestENG1729_SpanKindDiscoveryFansOutTwoUpstreamRequests(t *testing.T) {
	server, n := countingLogsServer(t)
	handler := NewGetLogAttributesForPipelineHandler(server.Client(), testAttrConfig(server.URL))
	_, _, _ = handler(context.Background(), &mcp.CallToolRequest{}, GetLogAttributesForPipelineArgs{
		Pipeline: []map[string]interface{}{
			{"type": "filter", "query": map[string]interface{}{
				"$and": []interface{}{
					map[string]interface{}{"$eq": []interface{}{"SpanKind", "SPAN_KIND_SERVER"}},
				},
			}},
		},
	})
	waitForRequests(t, n, 2)
	if got := n.Load(); got != 2 {
		t.Fatalf("ENG-1729 leak changed: SpanKind discovery made %d upstream requests, want 2 (current bug) / 0 (fixed)", got)
	}
}

func TestENG1729_MalformedGetLogsHitsUpstream(t *testing.T) {
	cases := []struct {
		name  string
		query []map[string]interface{}
	}{
		{
			name: "parse format instead of parser",
			query: []map[string]interface{}{
				{"type": "filter", "query": map[string]interface{}{"$and": []interface{}{map[string]interface{}{"$eq": []interface{}{"ServiceName", "checkout"}}}}},
				{"type": "parse", "format": "json"},
			},
		},
		{
			name: "window_aggregate aggregates+window_minutes",
			query: []map[string]interface{}{
				{"type": "filter", "query": map[string]interface{}{"$and": []interface{}{map[string]interface{}{"$eq": []interface{}{"ServiceName", "checkout"}}}}},
				{
					"type":           "window_aggregate",
					"aggregates":     []interface{}{map[string]interface{}{"function": map[string]interface{}{"$count": []interface{}{}}, "as": "_count"}},
					"window_minutes": 5,
				},
			},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			server, n := countingLogsServer(t)
			handler := NewGetLogsHandler(server.Client(), testLogsConfig(server.URL))
			_, _, _ = handler(context.Background(), &mcp.CallToolRequest{}, GetLogsArgs{
				LogjsonQuery:    tt.query,
				LookbackMinutes: 5,
			})
			if got := n.Load(); got < 1 {
				t.Fatalf("%s: expected at least 1 upstream request on current main, got %d", tt.name, got)
			}
		})
	}
}
