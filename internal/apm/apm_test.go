package apm

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/acrmp/mcp"
)

// const (
//
//	BaseURL      = "https://otlp-aps1.last9.io:443"
//	AuthToken    = "Basic <your-auth-token>"
//
// )
var (
	BaseURL      = "https://otlp-aps1.last9.io:443"
	AuthToken    = os.Getenv("TEST_AUTH_TOKEN")
	RefreshToken = os.Getenv("TEST_REFRESH_TOKEN")
)

func TestNewServiceSummaryHandler_ExtraParams(t *testing.T) {
	throughputResp := `{
		"data": {
			"result": [
				{
					"metric": {"service_name": "svc1"},
					"value": [1687600000, "10"]
				}
			]
		}
	}`
	responseTimeResp := `{
		"data": {
			"result": [
				{
					"metric": {"service_name": "svc1"},
					"value": [1687600000, "1.1"]
				}
			]
		}
	}`
	errorRateResp := `{
		"data": {
			"result": [
				{
					"metric": {"service_name": "svc1"},
					"value": [1687600000, "0.5"]
				}
			]
		}
	}`

	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		switch callCount {
		case 1:
			w.WriteHeader(http.StatusOK)
			io.WriteString(w, throughputResp)
		case 2:
			w.WriteHeader(http.StatusOK)
			io.WriteString(w, responseTimeResp)
		case 3:
			w.WriteHeader(http.StatusOK)
			io.WriteString(w, errorRateResp)
		}
	}))
	defer server.Close()

	cfg := models.Config{
		BaseURL:      BaseURL,
		AuthToken:    AuthToken,
		RefreshToken: RefreshToken,
	}
	if err := utils.PopulateAPICfg(&cfg); err != nil {
		t.Fatalf("failed to refresh access token: %v", err)
	}
	handler := NewServiceSummaryHandler(server.Client(), cfg)

	params := mcp.CallToolRequestParams{
		Arguments: map[string]any{
			"start_time":   time.Now().Add(-10 * time.Minute).UTC().Format(time.RFC3339),
			"end_time":     time.Now().UTC().Format(time.RFC3339),
			"extra_params": map[string]any{"span_kind": []any{"SPAN_KIND_SERVER", "SPAN_KIND_CLIENT"}},
		},
	}

	result, err := handler(params)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if len(result.Content) == 0 {
		t.Fatalf("expected content in result")
	}

	textContent, ok := result.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent type")
	}

	var summaries map[string]ServiceSummary
	if err := json.Unmarshal([]byte(textContent.Text), &summaries); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

}

// Add test for GetServicePerformanceDetails tool
func TestGetServicePerformanceDetails(t *testing.T) {

	cfg := models.Config{
		BaseURL:      BaseURL,
		AuthToken:    AuthToken,
		RefreshToken: RefreshToken,
	}
	if err := utils.PopulateAPICfg(&cfg); err != nil {
		t.Fatalf("failed to refresh access token: %v", err)
	}

	handler := NewServicePerformanceDetailsHandler(http.DefaultClient, cfg)

	params := mcp.CallToolRequestParams{
		Arguments: map[string]any{
			"service_name": "svc",
			"start_time":   time.Now().Add(-10 * time.Minute).UTC().Format(time.RFC3339),
			"end_time":     time.Now().UTC().Format(time.RFC3339),
			"environment":  "prod",
		},
	}

	result, err := handler(params)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if len(result.Content) == 0 {
		t.Fatalf("expected content in result")
	}

	textContent, ok := result.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent type")
	}

	var details ServicePerformanceDetails
	if err := json.Unmarshal([]byte(textContent.Text), &details); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

}

// Add test for GetServiceOperationsSummary tool
func TestGetServiceOperationsSummary(t *testing.T) {

	cfg := models.Config{
		BaseURL:      BaseURL,
		AuthToken:    AuthToken,
		RefreshToken: RefreshToken,
	}
	if err := utils.PopulateAPICfg(&cfg); err != nil {
		t.Fatalf("failed to refresh access token: %v", err)
	}

	handler := NewServiceOperationsSummaryHandler(http.DefaultClient, cfg)

	params := mcp.CallToolRequestParams{
		Arguments: map[string]any{
			"service_name": "svc",
			"start_time":   time.Now().Add(-10 * time.Minute).UTC().Format(time.RFC3339),
			"end_time":     time.Now().UTC().Format(time.RFC3339),
			"environment":  "prod",
		},
	}

	result, err := handler(params)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if len(result.Content) == 0 {
		t.Fatalf("expected content in result")
	}

	textContent, ok := result.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent type")
	}

	var details ServiceOperationsSummaryResponse
	if err := json.Unmarshal([]byte(textContent.Text), &details); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
}

// Add test for GetServiceDependencies tool
func TestGetServiceDependencies(t *testing.T) {

	cfg := models.Config{
		BaseURL:      BaseURL,
		AuthToken:    AuthToken,
		RefreshToken: RefreshToken,
	}
	if err := utils.PopulateAPICfg(&cfg); err != nil {
		t.Fatalf("failed to refresh access token: %v", err)
	}

	handler := NewServiceDependencyGraphHandler(http.DefaultClient, cfg)

	params := mcp.CallToolRequestParams{
		Arguments: map[string]any{
			"service_name": "svc",
			"start_time":   time.Now().Add(-60 * time.Minute).UTC().Format(time.RFC3339),
			"end_time":     time.Now().UTC().Format(time.RFC3339),
			"environment":  "prod",
		},
	}

	result, err := handler(params)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if len(result.Content) == 0 {
		t.Fatalf("expected content in result")
	}
	textContent, ok := result.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent type")
	}
	var details ServiceDependencyGraphDetails
	if err := json.Unmarshal([]byte(textContent.Text), &details); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
}

func TestNewServiceEnvironmentsHandler(t *testing.T) {
	cfg := models.Config{
		BaseURL:      BaseURL,
		AuthToken:    AuthToken,
		RefreshToken: RefreshToken,
	}
	if err := utils.PopulateAPICfg(&cfg); err != nil {
		t.Fatalf("failed to refresh access token: %v", err)
	}

	handler := NewServiceEnvironmentsHandler(http.DefaultClient, cfg)

	params := mcp.CallToolRequestParams{
		Arguments: map[string]any{
			"start_time": time.Now().Add(-10 * time.Minute).UTC().Format(time.RFC3339),
			"end_time":   time.Now().UTC().Format(time.RFC3339),
		},
	}

	result, err := handler(params)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if len(result.Content) == 0 {
		t.Fatalf("expected content in result")
	}
	textContent, ok := result.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent type")
	}
	var details []string
	if err := json.Unmarshal([]byte(textContent.Text), &details); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
}

// write test for promql instant query handler
func TestPromqlInstantQueryHandler(t *testing.T) {

	cfg := models.Config{
		BaseURL:      BaseURL,
		AuthToken:    AuthToken,
		RefreshToken: RefreshToken,
	}
	if err := utils.PopulateAPICfg(&cfg); err != nil {
		t.Fatalf("failed to refresh access token: %v", err)
	}

	handler := NewPromqlInstantQueryHandler(http.DefaultClient, cfg)

	params := mcp.CallToolRequestParams{
		Arguments: map[string]any{
			"query": "sum_over_time(trace_call_graph_count{}[1h])",
			"time":  time.Now().UTC().Format(time.RFC3339),
		},
	}

	result, err := handler(params)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if len(result.Content) == 0 {
		t.Fatalf("expected content in result")
	}
	_, ok := result.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent type")
	}
}

// write test for promql range query handler
func TestPromqlRangeQueryHandler(t *testing.T) {

	cfg := models.Config{
		BaseURL:      BaseURL,
		AuthToken:    AuthToken,
		RefreshToken: RefreshToken,
	}
	if err := utils.PopulateAPICfg(&cfg); err != nil {
		t.Fatalf("failed to refresh access token: %v", err)
	}

	handler := NewPromqlRangeQueryHandler(http.DefaultClient, cfg)

	params := mcp.CallToolRequestParams{
		Arguments: map[string]any{
			"query":      "sum(rate(http_request_duration_seconds_count[1m]))",
			"start_time": time.Now().Add(-10 * time.Minute).UTC().Format(time.RFC3339),
			"end_time":   time.Now().UTC().Format(time.RFC3339),
		},
	}

	result, err := handler(params)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if len(result.Content) == 0 {
		t.Fatalf("expected content in result")
	}
	textContent, ok := result.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent type")
	}
	var details []TimeSeries
	if err := json.Unmarshal([]byte(textContent.Text), &details); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

}

// write test for promql label values handler
func TestPromqlLabelValuesHandler(t *testing.T) {

}
