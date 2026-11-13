package traces

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestGetTracesIDFilterRequiresExplicitWindow(t *testing.T) {
	handler := NewGetTracesHandler(http.DefaultClient, models.Config{})
	query := []map[string]interface{}{{
		"type": "filter",
		"query": map[string]interface{}{
			"$eq": []interface{}{"TraceId", "0123456789abcdef0123456789abcdef"},
		},
	}}

	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetTracesArgs{TracejsonQuery: query})
	if err == nil {
		t.Fatal("expected TraceId filter without a window to fail")
	}
	for _, parameter := range []string{"lookback_minutes", "start_time_iso"} {
		if !strings.Contains(err.Error(), parameter) {
			t.Errorf("error %q does not name %s", err, parameter)
		}
	}
}

func TestTraceIDFilterWindowValidationScope(t *testing.T) {
	for _, tc := range []struct {
		name  string
		query []map[string]interface{}
		want  bool
	}{
		{"nested TraceId", []map[string]interface{}{{"type": "filter", "query": map[string]interface{}{"$and": []interface{}{map[string]interface{}{"$eq": []interface{}{"TraceId", "abc"}}}}}}, true},
		{"SpanId", []map[string]interface{}{{"type": "filter", "query": map[string]interface{}{"$eq": []interface{}{"SpanId", "abc"}}}}, false},
		{"TraceId outside filter", []map[string]interface{}{{"type": "transform", "expression": []interface{}{"TraceId"}}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := pipelineHasTraceIDFilter(tc.query); got != tc.want {
				t.Fatalf("pipelineHasTraceIDFilter() = %v, want %v", got, tc.want)
			}
		})
	}
}
