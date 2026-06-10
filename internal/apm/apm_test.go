package apm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
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

func TestResolveDatasourceCfg(t *testing.T) {
	cfg := models.Config{
		PrometheusReadURL:  "https://default.example.com/prom",
		PrometheusUsername: "default-user",
		PrometheusPassword: "default-pass",
		Datasources: []models.DatasourceInfo{
			{Name: "prod", ReadURL: "https://prod.example.com/prom", Username: "prod-user", Password: "prod-pass", Region: "us-east-1", ClusterID: "prod-cluster", IsDefault: true},
			{Name: "staging", ReadURL: "https://staging.example.com/prom", Username: "staging-user", Password: "staging-pass", Region: "ap-south-1", ClusterID: "staging-cluster"},
		},
	}

	t.Run("empty name returns original cfg unchanged", func(t *testing.T) {
		resolved, err := resolveDatasourceCfg(cfg, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resolved.PrometheusReadURL != cfg.PrometheusReadURL {
			t.Errorf("ReadURL = %q, want %q", resolved.PrometheusReadURL, cfg.PrometheusReadURL)
		}
	})

	t.Run("known datasource overrides prometheus credentials and region", func(t *testing.T) {
		resolved, err := resolveDatasourceCfg(cfg, "staging")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resolved.PrometheusReadURL != "https://staging.example.com/prom" {
			t.Errorf("ReadURL = %q, want staging URL", resolved.PrometheusReadURL)
		}
		if resolved.PrometheusUsername != "staging-user" {
			t.Errorf("Username = %q, want staging-user", resolved.PrometheusUsername)
		}
		if resolved.PrometheusPassword != "staging-pass" {
			t.Errorf("Password = %q, want staging-pass", resolved.PrometheusPassword)
		}
		if resolved.Region != "ap-south-1" {
			t.Errorf("Region = %q, want ap-south-1", resolved.Region)
		}
		if resolved.ClusterID != "staging-cluster" {
			t.Errorf("ClusterID = %q, want staging-cluster", resolved.ClusterID)
		}
	})

	t.Run("unknown datasource returns error containing the name", func(t *testing.T) {
		_, err := resolveDatasourceCfg(cfg, "nonexistent")
		if err == nil {
			t.Fatal("expected error for unknown datasource, got nil")
		}
		if !strings.Contains(err.Error(), "nonexistent") {
			t.Errorf("error %q does not mention the datasource name", err.Error())
		}
	})

	t.Run("original cfg is not mutated", func(t *testing.T) {
		_, _ = resolveDatasourceCfg(cfg, "staging")
		if cfg.PrometheusReadURL != "https://default.example.com/prom" {
			t.Error("original cfg was mutated by resolveDatasourceCfg")
		}
	})
}

func TestPromqlRangeHandler_DatasourceOverride(t *testing.T) {
	type capturedReq struct {
		ReadURL  string `json:"read_url"`
		Username string `json:"username"`
		Password string `json:"password"`
	}

	var (
		captured capturedReq
		hitCount int32
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hitCount, 1)
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &captured)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	cfg := models.Config{
		APIBaseURL:         server.URL,
		PrometheusReadURL:  "https://default.example.com/prom",
		PrometheusUsername: "default-user",
		PrometheusPassword: "default-pass",
		Datasources: []models.DatasourceInfo{
			{Name: "staging", ReadURL: "https://staging.example.com/prom", Username: "staging-user", Password: "staging-pass", Region: "us-east-1"},
		},
	}
	cfg.TokenManager = &auth.TokenManager{
		AccessToken: "mock-token",
		ExpiresAt:   time.Now().Add(time.Hour),
	}

	handler := NewPromqlRangeQueryHandler(server.Client(), cfg)

	// No datasource — default credentials should reach the server
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, PromqlRangeQueryArgs{
		Query: "up", LookbackMinutes: 5,
	})
	if err != nil {
		t.Fatalf("handler error (default): %v", err)
	}
	if captured.ReadURL != "https://default.example.com/prom" {
		t.Errorf("default: read_url = %q, want default URL", captured.ReadURL)
	}

	// With datasource override — staging credentials should reach the server
	_, _, err = handler(context.Background(), &mcp.CallToolRequest{}, PromqlRangeQueryArgs{
		Query: "up", LookbackMinutes: 5, Datasource: "staging",
	})
	if err != nil {
		t.Fatalf("handler error (staging): %v", err)
	}
	if captured.ReadURL != "https://staging.example.com/prom" {
		t.Errorf("staging: read_url = %q, want staging URL", captured.ReadURL)
	}
	if captured.Username != "staging-user" {
		t.Errorf("staging: username = %q, want staging-user", captured.Username)
	}
	if captured.Password != "staging-pass" {
		t.Errorf("staging: password = %q, want staging-pass", captured.Password)
	}

	// Unknown datasource — handler must return error before hitting the server
	beforeUnknown := atomic.LoadInt32(&hitCount)
	_, _, err = handler(context.Background(), &mcp.CallToolRequest{}, PromqlRangeQueryArgs{
		Query: "up", LookbackMinutes: 5, Datasource: "nonexistent",
	})
	if err == nil {
		t.Fatal("expected error for unknown datasource, got nil")
	}
	if after := atomic.LoadInt32(&hitCount); after != beforeUnknown {
		t.Fatalf("unknown datasource: server was contacted %d time(s), want 0", after-beforeUnknown)
	}
}

func TestNewListDatasourcesHandler(t *testing.T) {
	t.Run("returns all datasources with correct is_default flags", func(t *testing.T) {
		cfg := models.Config{
			Datasources: []models.DatasourceInfo{
				{Name: "prod", IsDefault: true},
				{Name: "staging", IsDefault: false},
			},
		}

		handler := NewListDatasourcesHandler(cfg)
		result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, ListDatasourcesArgs{})
		if err != nil {
			t.Fatalf("handler returned error: %v", err)
		}

		textContent, ok := result.Content[0].(*mcp.TextContent)
		if !ok {
			t.Fatal("expected TextContent")
		}

		var views []struct {
			Name      string `json:"name"`
			IsDefault bool   `json:"is_default"`
		}
		if err := json.Unmarshal([]byte(textContent.Text), &views); err != nil {
			t.Fatalf("failed to unmarshal response: %v", err)
		}
		if len(views) != 2 {
			t.Fatalf("expected 2 datasources, got %d", len(views))
		}
		if views[0].Name != "prod" || !views[0].IsDefault {
			t.Errorf("first entry: name=%q is_default=%v, want prod/true", views[0].Name, views[0].IsDefault)
		}
		if views[1].Name != "staging" || views[1].IsDefault {
			t.Errorf("second entry: name=%q is_default=%v, want staging/false", views[1].Name, views[1].IsDefault)
		}
	})

	t.Run("empty datasources list returns empty JSON array", func(t *testing.T) {
		handler := NewListDatasourcesHandler(models.Config{})
		result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, ListDatasourcesArgs{})
		if err != nil {
			t.Fatalf("handler returned error: %v", err)
		}
		textContent := result.Content[0].(*mcp.TextContent)
		if textContent.Text != "[]" {
			t.Errorf("empty list text = %q, want []", textContent.Text)
		}
	})
}

func TestNewPromqlLabelValuesHandler_MatchAlias(t *testing.T) {
	var captured []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Matches []string `json:"matches"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		captured = body.Matches
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `[]`)
	}))
	defer server.Close()

	cfg := models.Config{APIBaseURL: server.URL, Region: "us-east-1"}
	cfg.TokenManager = &auth.TokenManager{
		AccessToken: "mock-access-token-for-testing",
		ExpiresAt:   time.Now().Add(time.Hour),
	}
	handler := NewPromqlLabelValuesHandler(server.Client(), cfg)

	cases := []struct {
		name string
		args PromqlLabelValuesArgs
		want string
	}{
		{"match alias used when match_query empty", PromqlLabelValuesArgs{Match: "up", Label: "job"}, "up"},
		{"match_query wins over match", PromqlLabelValuesArgs{MatchQuery: "canonical", Match: "alias", Label: "job"}, "canonical"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			captured = nil
			_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, tc.args)
			if err != nil {
				t.Fatalf("handler error: %v", err)
			}
			if len(captured) == 0 || captured[0] != tc.want {
				t.Fatalf("backend received matches %v, want [%q]", captured, tc.want)
			}
		})
	}

	t.Run("neither match nor match_query errors", func(t *testing.T) {
		_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, PromqlLabelValuesArgs{Label: "job"})
		if err == nil || !strings.Contains(err.Error(), "match_query is required") {
			t.Fatalf("expected match_query required error, got: %v", err)
		}
	})
}

func TestNewPromqlLabelsHandler_MatchAlias(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `[]`)
	}))
	defer server.Close()

	cfg := models.Config{APIBaseURL: server.URL, Region: "us-east-1"}
	cfg.TokenManager = &auth.TokenManager{
		AccessToken: "mock-access-token-for-testing",
		ExpiresAt:   time.Now().Add(time.Hour),
	}
	handler := NewPromqlLabelsHandler(server.Client(), cfg)

	if _, _, err := handler(context.Background(), &mcp.CallToolRequest{}, PromqlLabelsArgs{Match: "up"}); err != nil {
		t.Fatalf("match alias should satisfy required match query, got: %v", err)
	}
	if _, _, err := handler(context.Background(), &mcp.CallToolRequest{}, PromqlLabelsArgs{}); err == nil {
		t.Fatal("expected error when neither match nor match_query set")
	}
}

func TestNewServiceSummaryHandler_ServiceFilter(t *testing.T) {
	var queries []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Query string `json:"query"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		queries = append(queries, body.Query)
		w.Header().Set("Content-Type", "application/json")
		// Non-empty result so the handler proceeds past the throughput query
		// and issues all three queries (empty throughput short-circuits).
		io.WriteString(w, `[{"metric":{"service_name":"svc1"},"value":[1687600000,"10"]}]`)
	}))
	defer server.Close()

	cfg := models.Config{APIBaseURL: server.URL, Region: "us-east-1"}
	cfg.TokenManager = &auth.TokenManager{
		AccessToken: "mock-access-token-for-testing",
		ExpiresAt:   time.Now().Add(time.Hour),
	}
	handler := NewServiceSummaryHandler(server.Client(), cfg)

	cases := []struct {
		name string
		args ServiceSummaryArgs
		want string
	}{
		{"service_name filters queries", ServiceSummaryArgs{ServiceName: "svc1"}, "service_name=~'svc1'"},
		{"service alias filters queries", ServiceSummaryArgs{Service: "svc2"}, "service_name=~'svc2'"},
		{"service_name wins over service", ServiceSummaryArgs{ServiceName: "canonical", Service: "alias"}, "service_name=~'canonical'"},
		{"omitted defaults to all services", ServiceSummaryArgs{}, "service_name=~'.*'"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			queries = nil
			_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, tc.args)
			if err != nil {
				t.Fatalf("handler error: %v", err)
			}
			if len(queries) != 3 {
				t.Fatalf("expected 3 queries (throughput, response time, error rate), got %d: %v", len(queries), queries)
			}
			for _, q := range queries {
				if !strings.Contains(q, tc.want) {
					t.Fatalf("query %q missing filter %q", q, tc.want)
				}
			}
		})
	}
}

func TestPromqlLabelValuesHandler_MatchAlias_Integration(t *testing.T) {
	cfg := utils.SetupTestConfigOrSkip(t)

	handler := NewPromqlLabelValuesHandler(http.DefaultClient, *cfg)
	ctx := context.Background()
	req := &mcp.CallToolRequest{}
	start := time.Now().Add(-1 * time.Hour).UTC().Format(time.RFC3339)
	end := time.Now().UTC().Format(time.RFC3339)

	canonical, _, err := handler(ctx, req, PromqlLabelValuesArgs{
		MatchQuery: "up", Label: "job", StartTimeISO: start, EndTimeISO: end,
	})
	if utils.CheckAPIError(t, err) {
		return
	}
	aliased, _, err := handler(ctx, req, PromqlLabelValuesArgs{
		Match: "up", Label: "job", StartTimeISO: start, EndTimeISO: end,
	})
	if utils.CheckAPIError(t, err) {
		return
	}

	canonicalText := utils.GetTextContent(t, canonical)
	aliasedText := utils.GetTextContent(t, aliased)
	if canonicalText != aliasedText {
		t.Fatalf("alias and canonical param returned different results:\ncanonical: %s\nalias: %s", canonicalText, aliasedText)
	}
	t.Logf("Integration test successful: match alias returns identical results to match_query")
}

func TestPromqlLabelsHandler_MatchAlias_Integration(t *testing.T) {
	cfg := utils.SetupTestConfigOrSkip(t)

	handler := NewPromqlLabelsHandler(http.DefaultClient, *cfg)
	ctx := context.Background()
	req := &mcp.CallToolRequest{}
	result, _, err := handler(ctx, req, PromqlLabelsArgs{
		Match:        "up",
		StartTimeISO: time.Now().Add(-1 * time.Hour).UTC().Format(time.RFC3339),
		EndTimeISO:   time.Now().UTC().Format(time.RFC3339),
	})
	if utils.CheckAPIError(t, err) {
		return
	}
	text := utils.GetTextContent(t, result)
	if text == "" {
		t.Fatal("expected non-empty labels response via match alias")
	}
	t.Logf("Integration test successful: match alias accepted by prometheus_labels")
}

func TestNewServiceSummaryHandler_ServiceFilter_Integration(t *testing.T) {
	cfg := utils.SetupTestConfigOrSkip(t)

	handler := NewServiceSummaryHandler(http.DefaultClient, *cfg)
	ctx := context.Background()
	req := &mcp.CallToolRequest{}
	args := ServiceSummaryArgs{
		StartTimeISO: time.Now().Add(-60 * time.Minute).UTC().Format(time.RFC3339),
		EndTimeISO:   time.Now().UTC().Format(time.RFC3339),
	}

	unfiltered, _, err := handler(ctx, req, args)
	if utils.CheckAPIError(t, err) {
		return
	}
	var all map[string]ServiceSummary
	if err := json.Unmarshal([]byte(utils.GetTextContent(t, unfiltered)), &all); err != nil {
		t.Fatalf("unfiltered response not JSON: %v", err)
	}
	if len(all) == 0 {
		t.Skip("no services in window; cannot exercise the filter")
	}

	var target string
	for name := range all {
		if name != "" {
			target = name
			break
		}
	}
	if target == "" {
		t.Skip("only empty service names in window")
	}

	args.ServiceName = target
	filtered, _, err := handler(ctx, req, args)
	if utils.CheckAPIError(t, err) {
		return
	}
	var got map[string]ServiceSummary
	if err := json.Unmarshal([]byte(utils.GetTextContent(t, filtered)), &got); err != nil {
		t.Fatalf("filtered response not JSON: %v", err)
	}
	for name := range got {
		if name != target {
			t.Fatalf("filter service_name=%q leaked service %q", target, name)
		}
	}
	if _, ok := got[target]; !ok {
		t.Fatalf("filter service_name=%q returned no entry for it: %v", target, got)
	}
	t.Logf("Integration test successful: filter restricted summary to %q (%d of %d services)", target, len(got), len(all))
}
