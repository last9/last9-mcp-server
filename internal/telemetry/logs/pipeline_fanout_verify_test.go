package logs

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestGetLogAttributesForPipeline_InvalidInputMakesZeroUpstreamRequests(t *testing.T) {
	server, n := countingLogsServer(t)
	handler := NewGetLogAttributesForPipelineHandler(server.Client(), testAttrConfig(server.URL))
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetLogAttributesForPipelineArgs{
		Pipeline: []map[string]interface{}{
			{"type": "filter", "query": map[string]interface{}{
				"$and": []interface{}{
					map[string]interface{}{"$eq": []interface{}{"SpanKind", "SPAN_KIND_SERVER"}},
				},
			}},
		},
	})
	if err == nil {
		t.Fatal("expected local validation error")
	}
	if got := n.Load(); got != 0 {
		t.Fatalf("SpanKind discovery made %d upstream requests, want 0", got)
	}
}

func TestGetLogs_InvalidPipelinesMakeZeroUpstreamRequests(t *testing.T) {
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
			_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetLogsArgs{
				LogjsonQuery:    tt.query,
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

func TestGetLogs_CanonicalWindowAggregateStillQueries(t *testing.T) {
	server, n := countingLogsServer(t)
	handler := NewGetLogsHandler(server.Client(), testLogsConfig(server.URL))
	_, _, _ = handler(context.Background(), &mcp.CallToolRequest{}, GetLogsArgs{
		LogjsonQuery: []map[string]interface{}{
			{"type": "filter", "query": map[string]interface{}{"$and": []interface{}{map[string]interface{}{"$eq": []interface{}{"ServiceName", "checkout"}}}}},
			{"type": "window_aggregate", "function": map[string]interface{}{"$count": []interface{}{}}, "as": "rate", "window": []interface{}{"5", "minutes"}},
		},
		LookbackMinutes: 5,
	})
	if got := n.Load(); got < 1 {
		t.Fatalf("canonical window_aggregate should still query upstream, got %d", got)
	}
}

func TestGetLogAttributesForPipeline_ValidPipelineStillSamples(t *testing.T) {
	var n atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/logs/api/v2/series/json") {
			_, _ = io.WriteString(w, `{"status":"success","data":[{"service":"checkout"}]}`)
			return
		}
		_, _ = io.WriteString(w, `{"status":"success","data":{"resultType":"streams","result":[]}}`)
	}))
	t.Cleanup(server.Close)

	handler := NewGetLogAttributesForPipelineHandler(server.Client(), testAttrConfig(server.URL))
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetLogAttributesForPipelineArgs{
		Pipeline: []map[string]interface{}{
			{"type": "filter", "query": map[string]interface{}{"$eq": []interface{}{"ServiceName", "checkout"}}},
		},
	})
	if err != nil {
		t.Fatalf("valid pipeline: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && n.Load() < 2 {
		time.Sleep(10 * time.Millisecond)
	}
	if got := n.Load(); got != 2 {
		t.Fatalf("valid discovery should keep parallel series+sample, got %d", got)
	}
}
