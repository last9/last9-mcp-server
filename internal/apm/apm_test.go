package apm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Note: strings import is still needed for TestNewServiceSummaryHandler_ExtraParams

func TestNewServiceSummaryHandler_ExtraParams(t *testing.T) {
	// Mock responses should match apiPromInstantResp format (direct array)
	throughputResp := `[
				{
					"metric": {"service_name": "svc1"},
					"value": [1687600000, "10"]
				}
	]`
	responseTimeResp := `[
				{
					"metric": {"service_name": "svc1"},
					"value": [1687600000, "1.1"]
				}
	]`
	errorRateResp := `[
				{
					"metric": {"service_name": "svc1"},
					"value": [1687600000, "0.5"]
				}
	]`

	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify we're hitting the prom_query_instant endpoint
		if !strings.Contains(r.URL.Path, "/prom_query_instant") {
			t.Errorf("Expected request to /prom_query_instant, got %s", r.URL.Path)
		}
		callCount++
		w.Header().Set("Content-Type", "application/json")
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
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	cfg := models.Config{
		APIBaseURL: server.URL,
		Region:     "us-east-1",
	}
	// Create a mock TokenManager for testing
	cfg.TokenManager = &auth.TokenManager{
		AccessToken: "mock-access-token-for-testing",
		ExpiresAt:   time.Now().Add(365 * 24 * time.Hour), // Valid for 1 year
	}
	handler := NewServiceSummaryHandler(server.Client(), cfg)

	args := ServiceSummaryArgs{
		StartTimeISO: time.Now().Add(-10 * time.Minute).UTC().Format(time.RFC3339),
		EndTimeISO:   time.Now().UTC().Format(time.RFC3339),
		Env:          "test",
	}

	ctx := context.Background()
	req := &mcp.CallToolRequest{}
	result, _, err := handler(ctx, req, args)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if len(result.Content) == 0 {
		t.Fatalf("expected content in result")
	}

	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent type")
	}

	var summaries map[string]ServiceSummary
	if err := json.Unmarshal([]byte(textContent.Text), &summaries); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

}

func TestGetServicePerformanceDetails(t *testing.T) {
	cfg := utils.SetupTestConfigOrSkip(t)

	handler := NewServicePerformanceDetailsHandler(http.DefaultClient, *cfg)

	args := ServicePerformanceDetailsArgs{
		ServiceName:  "svc",
		StartTimeISO: time.Now().Add(-10 * time.Minute).UTC().Format(time.RFC3339),
		EndTimeISO:   time.Now().UTC().Format(time.RFC3339),
		Env:          "prod",
	}

	ctx := context.Background()
	req := &mcp.CallToolRequest{}
	result, _, err := handler(ctx, req, args)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	text := utils.GetTextContent(t, result)

	var details ServicePerformanceDetails
	if err := json.Unmarshal([]byte(text), &details); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
}

func TestGetServiceOperationsSummary(t *testing.T) {
	cfg := utils.SetupTestConfigOrSkip(t)

	handler := NewServiceOperationsSummaryHandler(http.DefaultClient, *cfg)

	args := ServiceOperationsSummaryArgs{
		ServiceName:  "svc",
		StartTimeISO: time.Now().Add(-10 * time.Minute).UTC().Format(time.RFC3339),
		EndTimeISO:   time.Now().UTC().Format(time.RFC3339),
		Env:          "prod",
	}

	ctx := context.Background()
	req := &mcp.CallToolRequest{}
	result, _, err := handler(ctx, req, args)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	text := utils.GetTextContent(t, result)

	var details ServiceOperationsSummaryResponse
	if err := json.Unmarshal([]byte(text), &details); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
}

func TestGetServiceDependencies(t *testing.T) {
	cfg := utils.SetupTestConfigOrSkip(t)

	handler := NewServiceDependencyGraphHandler(http.DefaultClient, *cfg)

	args := ServiceDependencyGraphArgs{
		ServiceName:  "svc",
		StartTimeISO: time.Now().Add(-60 * time.Minute).UTC().Format(time.RFC3339),
		EndTimeISO:   time.Now().UTC().Format(time.RFC3339),
		Env:          "prod",
	}

	ctx := context.Background()
	req := &mcp.CallToolRequest{}
	result, _, err := handler(ctx, req, args)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	text := utils.GetTextContent(t, result)

	var details ServiceDependencyGraphDetails
	if err := json.Unmarshal([]byte(text), &details); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
}

func TestNewServiceEnvironmentsHandler(t *testing.T) {
	cfg := utils.SetupTestConfigOrSkip(t)

	handler := NewServiceEnvironmentsHandler(http.DefaultClient, *cfg)

	args := ServiceEnvironmentsArgs{
		StartTimeISO: time.Now().Add(-10 * time.Minute).UTC().Format(time.RFC3339),
		EndTimeISO:   time.Now().UTC().Format(time.RFC3339),
	}

	ctx := context.Background()
	req := &mcp.CallToolRequest{}
	result, _, err := handler(ctx, req, args)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	text := utils.GetTextContent(t, result)

	var details []string
	if err := json.Unmarshal([]byte(text), &details); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
}

func TestPromqlInstantQueryHandler(t *testing.T) {
	cfg := utils.SetupTestConfigOrSkip(t)

	handler := NewPromqlInstantQueryHandler(http.DefaultClient, *cfg)

	args := PromqlInstantQueryArgs{
		Query:   "sum_over_time(trace_call_graph_count{}[1h])",
		TimeISO: time.Now().UTC().Format(time.RFC3339),
	}

	ctx := context.Background()
	req := &mcp.CallToolRequest{}
	result, _, err := handler(ctx, req, args)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	_ = utils.GetTextContent(t, result)
}

func TestPromqlRangeQueryHandler(t *testing.T) {
	cfg := utils.SetupTestConfigOrSkip(t)

	handler := NewPromqlRangeQueryHandler(http.DefaultClient, *cfg)

	args := PromqlRangeQueryArgs{
		Query:        "sum(rate(http_request_duration_seconds_count[1m]))",
		StartTimeISO: time.Now().Add(-10 * time.Minute).UTC().Format(time.RFC3339),
		EndTimeISO:   time.Now().UTC().Format(time.RFC3339),
	}

	ctx := context.Background()
	req := &mcp.CallToolRequest{}
	result, _, err := handler(ctx, req, args)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	text := utils.GetTextContent(t, result)

	var details []TimeSeries
	if err := json.Unmarshal([]byte(text), &details); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
}

// Integration test for prometheus_labels tool
func TestPromqlLabelsHandler_Integration(t *testing.T) {
	cfg := utils.SetupTestConfigOrSkip(t)

	handler := NewPromqlLabelsHandler(http.DefaultClient, *cfg)

	args := PromqlLabelsArgs{
		MatchQuery: "up",
	}

	ctx := context.Background()
	req := &mcp.CallToolRequest{}
	result, _, err := handler(ctx, req, args)

	if utils.CheckAPIError(t, err) {
		return
	}

	text := utils.GetTextContent(t, result)

	var labels []string
	if err := json.Unmarshal([]byte(text), &labels); err != nil {
		t.Logf("Integration test successful. Response is formatted text (not JSON)")
	} else {
		t.Logf("Integration test successful: found %d label(s)", len(labels))
	}
}

func TestNewServiceSummaryHandler_Integration(t *testing.T) {
	cfg := utils.SetupTestConfigOrSkip(t)

	handler := NewServiceSummaryHandler(http.DefaultClient, *cfg)

	args := ServiceSummaryArgs{
		StartTimeISO: time.Now().Add(-10 * time.Minute).UTC().Format(time.RFC3339),
		EndTimeISO:   time.Now().UTC().Format(time.RFC3339),
		Env:          ".*",
	}

	ctx := context.Background()
	req := &mcp.CallToolRequest{}
	result, _, err := handler(ctx, req, args)

	if utils.CheckAPIError(t, err) {
		return
	}

	text := utils.GetTextContent(t, result)

	var summaries map[string]ServiceSummary
	if err := json.Unmarshal([]byte(text), &summaries); err != nil {
		t.Logf("Integration test successful. Response is formatted text (not JSON)")
	} else {
		t.Logf("Integration test successful: found %d service summary/ies", len(summaries))
	}
}

func TestPromqlLabelValuesHandler_Integration(t *testing.T) {
	cfg := utils.SetupTestConfigOrSkip(t)

	handler := NewPromqlLabelValuesHandler(http.DefaultClient, *cfg)

	args := PromqlLabelValuesArgs{
		MatchQuery:   "up",
		Label:        "job",
		StartTimeISO: time.Now().Add(-1 * time.Hour).UTC().Format(time.RFC3339),
		EndTimeISO:   time.Now().UTC().Format(time.RFC3339),
	}

	ctx := context.Background()
	req := &mcp.CallToolRequest{}
	result, _, err := handler(ctx, req, args)

	if utils.CheckAPIError(t, err) {
		return
	}

	text := utils.GetTextContent(t, result)

	var labelValues []string
	if err := json.Unmarshal([]byte(text), &labelValues); err != nil {
		t.Logf("Integration test successful. Response is formatted text (not JSON)")
	} else {
		t.Logf("Integration test successful: found %d label value(s) for label '%s'", len(labelValues), args.Label)
	}
}

func TestResolveDatasourceCfg_EmptyName(t *testing.T) {
	cfg := models.Config{
		PrometheusReadURL:  "http://default-prom:9090",
		PrometheusUsername: "default-user",
		PrometheusPassword: "default-pass",
	}

	result, err := resolveDatasourceCfg(context.Background(), http.DefaultClient, cfg, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.PrometheusReadURL != cfg.PrometheusReadURL {
		t.Errorf("expected PrometheusReadURL %q, got %q", cfg.PrometheusReadURL, result.PrometheusReadURL)
	}
	if result.PrometheusUsername != cfg.PrometheusUsername {
		t.Errorf("expected PrometheusUsername %q, got %q", cfg.PrometheusUsername, result.PrometheusUsername)
	}
	if result.PrometheusPassword != cfg.PrometheusPassword {
		t.Errorf("expected PrometheusPassword %q, got %q", cfg.PrometheusPassword, result.PrometheusPassword)
	}
}

func TestResolveDatasourceCfg_WithName(t *testing.T) {
	// Mock the datasources API endpoint
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, `[{"name":"my-prom","url":"http://other-prom:9090","properties":{"username":"other-user","password":"other-pass"}}]`)
	}))
	defer server.Close()

	cfg := models.Config{
		APIBaseURL:         server.URL,
		PrometheusReadURL:  "http://default-prom:9090",
		PrometheusUsername: "default-user",
		PrometheusPassword: "default-pass",
		TokenManager: &auth.TokenManager{
			AccessToken: "mock-token",
			ExpiresAt:   time.Now().Add(time.Hour),
		},
	}

	result, err := resolveDatasourceCfg(context.Background(), server.Client(), cfg, "my-prom")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.PrometheusReadURL != "http://other-prom:9090" {
		t.Errorf("expected PrometheusReadURL %q, got %q", "http://other-prom:9090", result.PrometheusReadURL)
	}
	if result.PrometheusUsername != "other-user" {
		t.Errorf("expected PrometheusUsername %q, got %q", "other-user", result.PrometheusUsername)
	}
	if result.PrometheusPassword != "other-pass" {
		t.Errorf("expected PrometheusPassword %q, got %q", "other-pass", result.PrometheusPassword)
	}
	// Original cfg should be unchanged (pass-by-value)
	if cfg.PrometheusReadURL != "http://default-prom:9090" {
		t.Errorf("original cfg was mutated: PrometheusReadURL = %q", cfg.PrometheusReadURL)
	}
}
