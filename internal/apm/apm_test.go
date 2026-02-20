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

func TestResolveTimeRange_Precedence(t *testing.T) {
	startISO := "2025-06-23 16:00:00"
	endISO := "2025-06-23 16:30:00"

	start, end, err := resolveTimeRange(startISO, endISO, 5)
	if err != nil {
		t.Fatalf("resolveTimeRange() returned error: %v", err)
	}
	if start != 1750694400 {
		t.Fatalf("start = %d, want %d", start, int64(1750694400))
	}
	if end != 1750696200 {
		t.Fatalf("end = %d, want %d", end, int64(1750696200))
	}

	start, end, err = resolveTimeRange("", endISO, 30)
	if err != nil {
		t.Fatalf("resolveTimeRange() end-only returned error: %v", err)
	}
	if end != 1750696200 {
		t.Fatalf("end-only end = %d, want %d", end, int64(1750696200))
	}
	if start != 1750694400 {
		t.Fatalf("end-only start = %d, want %d", start, int64(1750694400))
	}

	start, end, err = resolveTimeRange(startISO, "", 45)
	if err != nil {
		t.Fatalf("resolveTimeRange() start-only returned error: %v", err)
	}
	if start != 1750694400 {
		t.Fatalf("start-only start = %d, want %d", start, int64(1750694400))
	}
	if end != 1750697100 {
		t.Fatalf("start-only end = %d, want %d", end, int64(1750697100))
	}
}

func TestResolveInstantQueryTime(t *testing.T) {
	timeParam, err := resolveInstantQueryTime("2025-06-23T16:00:00Z", 30)
	if err != nil {
		t.Fatalf("resolveInstantQueryTime() returned error: %v", err)
	}
	if timeParam != 1750694400 {
		t.Fatalf("timeParam = %d, want %d", timeParam, int64(1750694400))
	}

	timeParam, err = resolveInstantQueryTime("", 30)
	if err != nil {
		t.Fatalf("resolveInstantQueryTime() lookback returned error: %v", err)
	}
	expected := time.Now().UTC().Add(-30 * time.Minute).Unix()
	if timeParam < expected-5 || timeParam > expected+5 {
		t.Fatalf("lookback timeParam = %d, expected near %d", timeParam, expected)
	}
}

func TestPromqlRangeHandler_UsesLookbackAndExplicitPrecedence(t *testing.T) {
	type capturedReq struct {
		Timestamp int64 `json:"timestamp"`
		Window    int64 `json:"window"`
	}

	var captured []capturedReq
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/prom_query") {
			t.Fatalf("expected prom_query endpoint, got %s", r.URL.Path)
		}

		body, _ := io.ReadAll(r.Body)
		var reqPayload capturedReq
		_ = json.Unmarshal(body, &reqPayload)
		captured = append(captured, reqPayload)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	cfg := models.Config{
		APIBaseURL: server.URL,
		Region:     "us-east-1",
	}
	cfg.TokenManager = &auth.TokenManager{
		AccessToken: "mock-access-token",
		ExpiresAt:   time.Now().Add(365 * 24 * time.Hour),
	}

	handler := NewPromqlRangeQueryHandler(server.Client(), cfg)

	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, PromqlRangeQueryArgs{
		Query:           "sum(rate(http_request_duration_seconds_count[1m]))",
		LookbackMinutes: 30,
	})
	if err != nil {
		t.Fatalf("handler returned error for lookback mode: %v", err)
	}

	if len(captured) != 1 {
		t.Fatalf("expected 1 captured request, got %d", len(captured))
	}
	if captured[0].Window != 1800 {
		t.Fatalf("window = %d, want %d", captured[0].Window, int64(1800))
	}

	_, _, err = handler(context.Background(), &mcp.CallToolRequest{}, PromqlRangeQueryArgs{
		Query:           "sum(rate(http_request_duration_seconds_count[1m]))",
		StartTimeISO:    "2025-06-23T16:00:00Z",
		EndTimeISO:      "2025-06-23T16:10:00Z",
		LookbackMinutes: 30,
	})
	if err != nil {
		t.Fatalf("handler returned error for explicit mode: %v", err)
	}

	if len(captured) != 2 {
		t.Fatalf("expected 2 captured requests, got %d", len(captured))
	}
	if captured[1].Window != 600 {
		t.Fatalf("window with explicit timestamps = %d, want %d", captured[1].Window, int64(600))
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
