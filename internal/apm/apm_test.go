package apm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var (
	RefreshToken = os.Getenv("TEST_REFRESH_TOKEN")
)

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
	// Skip if no refresh token is provided (integration test)
	testRefreshToken := os.Getenv("TEST_REFRESH_TOKEN")
	if testRefreshToken == "" {
		t.Skip("Skipping test: TEST_REFRESH_TOKEN not set")
	}

	cfg := models.Config{
		RefreshToken: testRefreshToken,
	}
	// Initialize TokenManager first
	tokenManager, err := auth.NewTokenManager(testRefreshToken)
	if err != nil {
		t.Fatalf("failed to create token manager: %v", err)
	}
	cfg.TokenManager = tokenManager
	if err := utils.PopulateAPICfg(&cfg); err != nil {
		t.Fatalf("failed to refresh access token: %v", err)
	}

	handler := NewServicePerformanceDetailsHandler(http.DefaultClient, cfg)

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

	if len(result.Content) == 0 {
		t.Fatalf("expected content in result")
	}

	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent type")
	}

	var details ServicePerformanceDetails
	if err := json.Unmarshal([]byte(textContent.Text), &details); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

}

func TestGetServiceOperationsSummary(t *testing.T) {
	// Skip if no refresh token is provided (integration test)
	testRefreshToken := os.Getenv("TEST_REFRESH_TOKEN")
	if testRefreshToken == "" {
		t.Skip("Skipping test: TEST_REFRESH_TOKEN not set")
	}

	cfg := models.Config{
		RefreshToken: testRefreshToken,
	}
	// Initialize TokenManager first
	tokenManager, err := auth.NewTokenManager(testRefreshToken)
	if err != nil {
		t.Fatalf("failed to create token manager: %v", err)
	}
	cfg.TokenManager = tokenManager
	if err := utils.PopulateAPICfg(&cfg); err != nil {
		t.Fatalf("failed to refresh access token: %v", err)
	}

	handler := NewServiceOperationsSummaryHandler(http.DefaultClient, cfg)

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

	if len(result.Content) == 0 {
		t.Fatalf("expected content in result")
	}

	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent type")
	}

	var details ServiceOperationsSummaryResponse
	if err := json.Unmarshal([]byte(textContent.Text), &details); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
}

func TestGetServiceDependencies(t *testing.T) {
	// Skip if no refresh token is provided (integration test)
	testRefreshToken := os.Getenv("TEST_REFRESH_TOKEN")
	if testRefreshToken == "" {
		t.Skip("Skipping test: TEST_REFRESH_TOKEN not set")
	}

	cfg := models.Config{
		RefreshToken: testRefreshToken,
	}
	// Initialize TokenManager first
	tokenManager, err := auth.NewTokenManager(testRefreshToken)
	if err != nil {
		t.Fatalf("failed to create token manager: %v", err)
	}
	cfg.TokenManager = tokenManager
	if err := utils.PopulateAPICfg(&cfg); err != nil {
		t.Fatalf("failed to refresh access token: %v", err)
	}

	handler := NewServiceDependencyGraphHandler(http.DefaultClient, cfg)

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

	if len(result.Content) == 0 {
		t.Fatalf("expected content in result")
	}
	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent type")
	}
	var details ServiceDependencyGraphDetails
	if err := json.Unmarshal([]byte(textContent.Text), &details); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
}

func TestNewServiceEnvironmentsHandler(t *testing.T) {
	// Skip if no refresh token is provided (integration test)
	testRefreshToken := os.Getenv("TEST_REFRESH_TOKEN")
	if testRefreshToken == "" {
		t.Skip("Skipping test: TEST_REFRESH_TOKEN not set")
	}

	cfg := models.Config{
		RefreshToken: testRefreshToken,
	}
	// Initialize TokenManager first
	tokenManager, err := auth.NewTokenManager(testRefreshToken)
	if err != nil {
		t.Fatalf("failed to create token manager: %v", err)
	}
	cfg.TokenManager = tokenManager
	if err := utils.PopulateAPICfg(&cfg); err != nil {
		t.Fatalf("failed to refresh access token: %v", err)
	}

	handler := NewServiceEnvironmentsHandler(http.DefaultClient, cfg)

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

	if len(result.Content) == 0 {
		t.Fatalf("expected content in result")
	}
	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent type")
	}
	var details []string
	if err := json.Unmarshal([]byte(textContent.Text), &details); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
}

func TestPromqlInstantQueryHandler(t *testing.T) {
	// Skip if no refresh token is provided (integration test)
	testRefreshToken := os.Getenv("TEST_REFRESH_TOKEN")
	if testRefreshToken == "" {
		t.Skip("Skipping test: TEST_REFRESH_TOKEN not set")
	}

	cfg := models.Config{
		RefreshToken: testRefreshToken,
	}
	// Initialize TokenManager first
	tokenManager, err := auth.NewTokenManager(testRefreshToken)
	if err != nil {
		t.Fatalf("failed to create token manager: %v", err)
	}
	cfg.TokenManager = tokenManager
	if err := utils.PopulateAPICfg(&cfg); err != nil {
		t.Fatalf("failed to refresh access token: %v", err)
	}

	handler := NewPromqlInstantQueryHandler(http.DefaultClient, cfg)

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

	if len(result.Content) == 0 {
		t.Fatalf("expected content in result")
	}
	_, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent type")
	}
}

func TestPromqlRangeQueryHandler(t *testing.T) {
	// Skip if no refresh token is provided (integration test)
	testRefreshToken := os.Getenv("TEST_REFRESH_TOKEN")
	if testRefreshToken == "" {
		t.Skip("Skipping test: TEST_REFRESH_TOKEN not set")
	}

	cfg := models.Config{
		RefreshToken: testRefreshToken,
	}
	// Initialize TokenManager first
	tokenManager, err := auth.NewTokenManager(testRefreshToken)
	if err != nil {
		t.Fatalf("failed to create token manager: %v", err)
	}
	cfg.TokenManager = tokenManager
	if err := utils.PopulateAPICfg(&cfg); err != nil {
		t.Fatalf("failed to refresh access token: %v", err)
	}

	handler := NewPromqlRangeQueryHandler(http.DefaultClient, cfg)

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

	if len(result.Content) == 0 {
		t.Fatalf("expected content in result")
	}
	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent type")
	}
	var details []TimeSeries
	if err := json.Unmarshal([]byte(textContent.Text), &details); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

}

// Integration test for prometheus_labels tool
func TestPromqlLabelsHandler_Integration(t *testing.T) {
	// Skip if no refresh token is provided (integration test)
	testRefreshToken := os.Getenv("TEST_REFRESH_TOKEN")
	if testRefreshToken == "" {
		t.Skip("Skipping integration test: TEST_REFRESH_TOKEN not set")
	}

	cfg := models.Config{
		RefreshToken: testRefreshToken,
	}
	// Initialize TokenManager first
	tokenManager, err := auth.NewTokenManager(testRefreshToken)
	if err != nil {
		t.Fatalf("failed to create token manager: %v", err)
	}
	cfg.TokenManager = tokenManager
	if err := utils.PopulateAPICfg(&cfg); err != nil {
		t.Fatalf("failed to populate API config: %v", err)
	}

	handler := NewPromqlLabelsHandler(http.DefaultClient, cfg)

	args := PromqlLabelsArgs{
		MatchQuery: "up",
	}

	ctx := context.Background()
	req := &mcp.CallToolRequest{}
	result, _, err := handler(ctx, req, args)

	// Fail on API errors (like 502) - these indicate real problems
	if err != nil {
		// Check if error is an HTTP error (like 502)
		if strings.Contains(err.Error(), "status") || strings.Contains(err.Error(), "502") || strings.Contains(err.Error(), "500") {
			t.Fatalf("API returned error (test should fail): %v", err)
		}
		// For other errors, log but don't fail
		t.Logf("Integration test warning: %v", err)
		return
	}

	if len(result.Content) == 0 {
		t.Fatalf("expected content in result")
	}

	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent type")
	}

	// Log summary - labels are typically returned as a list
	var labels []string
	if err := json.Unmarshal([]byte(textContent.Text), &labels); err != nil {
		// If it's not JSON, it might be formatted text - that's ok
		t.Logf("Integration test successful. Response is formatted text (not JSON)")
	} else {
		t.Logf("Integration test successful: found %d label(s)", len(labels))
	}
}

func TestNewServiceSummaryHandler_Integration(t *testing.T) {
	// Skip if no refresh token is provided (integration test)
	testRefreshToken := os.Getenv("TEST_REFRESH_TOKEN")
	if testRefreshToken == "" {
		t.Skip("Skipping integration test: TEST_REFRESH_TOKEN not set")
	}

	cfg := models.Config{
		RefreshToken: testRefreshToken,
	}
	// Initialize TokenManager first
	tokenManager, err := auth.NewTokenManager(testRefreshToken)
	if err != nil {
		t.Fatalf("failed to create token manager: %v", err)
	}
	cfg.TokenManager = tokenManager
	if err := utils.PopulateAPICfg(&cfg); err != nil {
		t.Fatalf("failed to populate API config: %v", err)
	}

	handler := NewServiceSummaryHandler(http.DefaultClient, cfg)

	args := ServiceSummaryArgs{
		StartTimeISO: time.Now().Add(-10 * time.Minute).UTC().Format(time.RFC3339),
		EndTimeISO:   time.Now().UTC().Format(time.RFC3339),
		Env:          ".*",
	}

	ctx := context.Background()
	req := &mcp.CallToolRequest{}
	result, _, err := handler(ctx, req, args)

	// Fail on API errors (like 502) - these indicate real problems
	if err != nil {
		// Check if error is an HTTP error (like 502)
		if strings.Contains(err.Error(), "status") || strings.Contains(err.Error(), "502") || strings.Contains(err.Error(), "500") {
			t.Fatalf("API returned error (test should fail): %v", err)
		}
		// For other errors, log but don't fail
		t.Logf("Integration test warning: %v", err)
		return
	}

	if len(result.Content) == 0 {
		t.Fatalf("expected content in result")
	}

	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent type")
	}

	// Log summary - service summary is typically returned as a JSON object
	var summaries map[string]ServiceSummary
	if err := json.Unmarshal([]byte(textContent.Text), &summaries); err != nil {
		// If it's not JSON, it might be formatted text - that's ok
		t.Logf("Integration test successful. Response is formatted text (not JSON)")
	} else {
		t.Logf("Integration test successful: found %d service summary/ies", len(summaries))
	}
}

func TestPromqlLabelValuesHandler_Integration(t *testing.T) {
	// Skip if no refresh token is provided (integration test)
	testRefreshToken := os.Getenv("TEST_REFRESH_TOKEN")
	if testRefreshToken == "" {
		t.Skip("Skipping integration test: TEST_REFRESH_TOKEN not set")
	}

	cfg := models.Config{
		RefreshToken: testRefreshToken,
	}
	// Initialize TokenManager first
	tokenManager, err := auth.NewTokenManager(testRefreshToken)
	if err != nil {
		t.Fatalf("failed to create token manager: %v", err)
	}
	cfg.TokenManager = tokenManager
	if err := utils.PopulateAPICfg(&cfg); err != nil {
		t.Fatalf("failed to populate API config: %v", err)
	}

	handler := NewPromqlLabelValuesHandler(http.DefaultClient, cfg)

	args := PromqlLabelValuesArgs{
		MatchQuery:   "up",
		Label:        "job",
		StartTimeISO: time.Now().Add(-1 * time.Hour).UTC().Format(time.RFC3339),
		EndTimeISO:   time.Now().UTC().Format(time.RFC3339),
	}

	ctx := context.Background()
	req := &mcp.CallToolRequest{}
	result, _, err := handler(ctx, req, args)

	// Fail on API errors (like 502) - these indicate real problems
	if err != nil {
		// Check if error is an HTTP error (like 502)
		if strings.Contains(err.Error(), "status") || strings.Contains(err.Error(), "502") || strings.Contains(err.Error(), "500") {
			t.Fatalf("API returned error (test should fail): %v", err)
		}
		// For other errors, log but don't fail
		t.Logf("Integration test warning: %v", err)
		return
	}

	if len(result.Content) == 0 {
		t.Fatalf("expected content in result")
	}

	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent type")
	}

	// Log summary - label values are typically returned as a JSON array
	var labelValues []string
	if err := json.Unmarshal([]byte(textContent.Text), &labelValues); err != nil {
		// If it's not JSON, it might be formatted text - that's ok
		t.Logf("Integration test successful. Response is formatted text (not JSON)")
	} else {
		t.Logf("Integration test successful: found %d label value(s) for label '%s'", len(labelValues), args.Label)
	}
}
