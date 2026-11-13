package logs

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestGetLogsDefaultWindowIsExplicitInResult(t *testing.T) {
	srv, _ := streamSearchServer(t, nil)
	query := []map[string]interface{}{{
		"type":  "filter",
		"query": map[string]interface{}{"$eq": []interface{}{"ServiceName", "checkout"}},
	}}
	result, _, err := NewGetLogsHandler(srv.Client(), apiModeCfg(srv.URL))(
		context.Background(), &mcp.CallToolRequest{}, GetLogsArgs{LogjsonQuery: query},
	)
	if err != nil {
		t.Fatal(err)
	}
	var response map[string]any
	if err := json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &response); err != nil {
		t.Fatal(err)
	}
	if response["window_source"] != "default" || response["window_minutes"] != float64(defaultGetLogsLookbackMinutes) {
		t.Fatalf("window metadata = source %v, minutes %v", response["window_source"], response["window_minutes"])
	}
	guidance, _ := response["l9_guidance"].(map[string]any)
	message, _ := guidance["message"].(string)
	if !strings.Contains(message, "No matches in the last 5 minutes (default window)") {
		t.Fatalf("guidance message = %q", message)
	}
}

func TestServiceLogsDefaultWindowEmptyResultIsExplicit(t *testing.T) {
	response := &ServiceLogsResponse{
		StartTime:     "2026-11-13T13:00:00Z",
		EndTime:       "2026-11-13T14:00:00Z",
		Logs:          []LogEntry{},
		WindowSource:  "default",
		WindowMinutes: 60,
	}
	addServiceLogsGuidance(response, GetServiceLogsArgs{ServiceName: "checkout"}, 100)
	if !strings.Contains(response.Hint, "No matches in the last 60 minutes (default window)") {
		t.Fatalf("hint = %q", response.Hint)
	}
}

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
