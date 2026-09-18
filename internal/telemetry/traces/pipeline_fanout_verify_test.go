package traces

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func countingTracesServer(t *testing.T) (*httptest.Server, *atomic.Int32) {
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

func TestGetTraces_InvalidPipelinesMakeZeroUpstreamRequests(t *testing.T) {
	cases := []struct {
		name  string
		query []map[string]interface{}
	}{
		{
			name: "unknown stage type",
			query: []map[string]interface{}{
				{"type": "trace_filter"},
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
		{
			name: "aggregate swapped quantile arguments",
			query: []map[string]interface{}{{
				"type": "aggregate",
				"aggregates": []interface{}{map[string]interface{}{
					"function": map[string]interface{}{"$quantile": []interface{}{"Duration", 0.95}}, "as": "p95",
				}},
			}},
		},
		{
			name: "window aggregate out-of-range quantile",
			query: []map[string]interface{}{{
				"type": "window_aggregate", "function": map[string]interface{}{"$quantile": []interface{}{1.01, "Duration"}},
				"as": "p95", "window": []interface{}{"5", "minutes"},
			}},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			server, n := countingTracesServer(t)
			handler := NewGetTracesHandler(server.Client(), testChunkTracesConfig(server.URL))
			_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetTracesArgs{
				TracejsonQuery:  tt.query,
				LookbackMinutes: 5,
			})
			if err == nil {
				t.Fatal("expected local validation error")
			}
			if got := n.Load(); got != 0 {
				t.Fatalf("%s made %d upstream requests, want 0", tt.name, got)
			}
		})
	}
}

func TestGetTraces_CanonicalWindowAggregateStillQueries(t *testing.T) {
	server, n := countingTracesServer(t)
	handler := NewGetTracesHandler(server.Client(), testChunkTracesConfig(server.URL))
	_, _, _ = handler(context.Background(), &mcp.CallToolRequest{}, GetTracesArgs{
		TracejsonQuery: []map[string]interface{}{
			{"type": "filter", "query": map[string]interface{}{"$and": []interface{}{map[string]interface{}{"$eq": []interface{}{"ServiceName", "checkout"}}}}},
			{"type": "window_aggregate", "function": map[string]interface{}{"$count": []interface{}{}}, "as": "rate", "window": []interface{}{"5", "minutes"}},
		},
		LookbackMinutes: 5,
	})
	if got := n.Load(); got < 1 {
		t.Fatalf("canonical window_aggregate should still query upstream, got %d", got)
	}
}
