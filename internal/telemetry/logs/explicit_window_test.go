package logs

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestGetLogsBodySearchRequiresExplicitWindow(t *testing.T) {
	handler := NewGetLogsHandler(http.DefaultClient, models.Config{})
	query := []map[string]interface{}{{
		"type": "filter",
		"query": map[string]interface{}{
			"$contains": []interface{}{"Body", "correlation-id"},
		},
	}}

	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetLogsArgs{LogjsonQuery: query})
	if err == nil {
		t.Fatal("expected body search without a window to fail")
	}
	assertExplicitWindowError(t, err)
}

func TestGetServiceLogsBodyFiltersRequireExplicitWindow(t *testing.T) {
	handler := NewGetServiceLogsHandler(http.DefaultClient, models.Config{})
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetServiceLogsArgs{
		ServiceName: "checkout",
		BodyFilters: []string{"correlation-id"},
	})
	if err == nil {
		t.Fatal("expected body_filters without a window to fail")
	}
	assertExplicitWindowError(t, err)
}

func TestBodySearchWindowValidationScope(t *testing.T) {
	for _, tc := range []struct {
		name  string
		query []map[string]interface{}
		want  bool
	}{
		{"nested contains words", []map[string]interface{}{{"type": "filter", "query": map[string]interface{}{"$and": []interface{}{map[string]interface{}{"$containsWords": []interface{}{"Body", "request failed"}}}}}}, true},
		{"case insensitive contains", []map[string]interface{}{{"type": "filter", "query": map[string]interface{}{"$icontains": []interface{}{"Body", "request-id"}}}}, true},
		{"body equality", []map[string]interface{}{{"type": "filter", "query": map[string]interface{}{"$eq": []interface{}{"Body", "ready"}}}}, false},
		{"attribute contains", []map[string]interface{}{{"type": "filter", "query": map[string]interface{}{"$contains": []interface{}{"attributes['path']", "/health"}}}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := pipelineHasBodySearch(tc.query); got != tc.want {
				t.Fatalf("pipelineHasBodySearch() = %v, want %v", got, tc.want)
			}
		})
	}
}

func assertExplicitWindowError(t *testing.T, err error) {
	t.Helper()
	for _, parameter := range []string{"lookback_minutes", "start_time_iso"} {
		if !strings.Contains(err.Error(), parameter) {
			t.Errorf("error %q does not name %s", err, parameter)
		}
	}
}
