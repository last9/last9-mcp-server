package apm

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func linkTestConfig(serverURL string) models.Config {
	return models.Config{
		APIBaseURL: serverURL,
		Region:     "us-east-1",
		OrgSlug:    "acme",
		ClusterID:  "cluster-1",
		TokenManager: &auth.TokenManager{
			AccessToken: "test-token",
			ExpiresAt:   time.Now().Add(24 * time.Hour),
		},
	}
}

func staticServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(server.Close)
	return server
}

func referenceURL(t *testing.T, res *mcp.CallToolResult) *url.URL {
	t.Helper()
	if res == nil {
		t.Fatal("nil result")
	}
	raw, ok := res.Meta["reference_url"].(string)
	if !ok || raw == "" {
		t.Fatalf("missing reference_url in meta: %v", res.Meta)
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse reference_url %q: %v", raw, err)
	}
	return u
}

func assertQueryParams(t *testing.T, u *url.URL, want map[string]string) {
	t.Helper()
	q := u.Query()
	for k, v := range want {
		if got := q.Get(k); got != v {
			t.Errorf("%s = %q, want %q (url %s)", k, got, v, u)
		}
	}
}

func TestPromqlRangeQueryHandler_MetricsLink(t *testing.T) {
	server := staticServer(t, http.StatusOK, `[]`)
	handler := NewPromqlRangeQueryHandler(server.Client(), linkTestConfig(server.URL))

	query := `sum(rate(http_requests_total[5m]))`
	res, _, err := handler(context.Background(), &mcp.CallToolRequest{}, PromqlRangeQueryArgs{
		Query:        query,
		StartTimeISO: "2026-02-09T15:04:05Z",
		EndTimeISO:   "2026-02-09T16:04:05Z",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	u := referenceURL(t, res)
	if u.Path != "/v2/organizations/acme/metrics" {
		t.Fatalf("path = %q", u.Path)
	}
	assertQueryParams(t, u, map[string]string{
		"from":    "1770649445",
		"to":      "1770653045",
		"cluster": "cluster-1",
		"mode":    "code",
		"query":   query,
	})
}

func TestPromqlRangeQueryHandler_NoLinkOnUpstreamError(t *testing.T) {
	server := staticServer(t, http.StatusUnprocessableEntity, `{"error":"bad query"}`)
	handler := NewPromqlRangeQueryHandler(server.Client(), linkTestConfig(server.URL))

	res, _, err := handler(context.Background(), &mcp.CallToolRequest{}, PromqlRangeQueryArgs{
		Query:           "up",
		LookbackMinutes: 10,
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res == nil || !res.IsError {
		t.Fatalf("expected tool error result, got %+v", res)
	}
	if _, ok := res.Meta["reference_url"]; ok {
		t.Fatalf("error result must not carry a link: %v", res.Meta)
	}
}

func TestPromqlInstantQueryHandler_MetricsLinkEndsAtEvaluationTime(t *testing.T) {
	server := staticServer(t, http.StatusOK, `[]`)
	handler := NewPromqlInstantQueryHandler(server.Client(), linkTestConfig(server.URL))

	res, _, err := handler(context.Background(), &mcp.CallToolRequest{}, PromqlInstantQueryArgs{
		Query:   "up",
		TimeISO: "2026-02-09T16:04:05Z",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	u := referenceURL(t, res)
	if u.Path != "/v2/organizations/acme/metrics" {
		t.Fatalf("path = %q", u.Path)
	}
	assertQueryParams(t, u, map[string]string{
		"from":    "1770652145", // 15 minutes before the evaluation time
		"to":      "1770653045",
		"cluster": "cluster-1",
		"mode":    "code",
		"query":   "up",
	})
}

func TestServiceEnvironmentsHandler_ServiceCatalogLink(t *testing.T) {
	server := staticServer(t, http.StatusOK, `["prod"]`)
	handler := NewServiceEnvironmentsHandler(server.Client(), linkTestConfig(server.URL))

	res, _, err := handler(context.Background(), &mcp.CallToolRequest{}, ServiceEnvironmentsArgs{
		StartTimeISO: "2026-02-09T15:04:05Z",
		EndTimeISO:   "2026-02-09T16:04:05Z",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	u := referenceURL(t, res)
	if u.Path != "/v2/organizations/acme/service-catalog" {
		t.Fatalf("path = %q", u.Path)
	}
	assertQueryParams(t, u, map[string]string{
		"from":    "1770649445",
		"to":      "1770653045",
		"cluster": "cluster-1",
	})
}

func useFakeNow(t *testing.T, iso string) {
	t.Helper()
	t.Cleanup(func() { t.Setenv(utils.FakeNowEnv, ""); _, _ = utils.InitClock() })
	t.Setenv(utils.FakeNowEnv, iso)
	if _, err := utils.InitClock(); err != nil {
		t.Fatal(err)
	}
}

func TestGetMetricStatusHandler_MetricsLink(t *testing.T) {
	useFakeNow(t, "2026-02-09T16:04:05Z")
	server := staticServer(t, http.StatusOK, `[]`)
	handler := NewGetMetricStatusHandler(server.Client(), linkTestConfig(server.URL))

	res, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetMetricStatusArgs{
		Metric:        "http_requests_total",
		WindowMinutes: 60,
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	u := referenceURL(t, res)
	if u.Path != "/v2/organizations/acme/metrics" {
		t.Fatalf("path = %q", u.Path)
	}
	assertQueryParams(t, u, map[string]string{
		"from":  "1770649445",
		"to":    "1770653045",
		"mode":  "code",
		"query": `{__name__="http_requests_total"}`,
	})
}

func TestGetMetricStatusHandler_MetricsLinkUsesChosenDatasourceCluster(t *testing.T) {
	useFakeNow(t, "2026-02-09T16:04:05Z")
	server := staticServer(t, http.StatusOK, `[]`)
	cfg := linkTestConfig(server.URL)
	cfg.Datasources = []models.DatasourceInfo{
		{Name: "prod-eu", ReadURL: "https://prod-eu.example.com/prom", Username: "u", Password: "p", Region: "eu-west-1", ClusterID: "cluster-eu"},
	}
	handler := NewGetMetricStatusHandler(server.Client(), cfg)

	res, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetMetricStatusArgs{
		Metric:        "up",
		WindowMinutes: 60,
		Datasource:    "prod-eu",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	assertQueryParams(t, referenceURL(t, res), map[string]string{"cluster": "cluster-eu"})
}

func TestGetMetricStatusHandler_NoLinkOnUpstreamError(t *testing.T) {
	server := staticServer(t, http.StatusUnprocessableEntity, `{"error":"bad query"}`)
	handler := NewGetMetricStatusHandler(server.Client(), linkTestConfig(server.URL))

	res, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetMetricStatusArgs{Metric: "up", WindowMinutes: 5})
	if err == nil {
		t.Fatalf("expected an error, got %+v", res)
	}
	if res != nil && res.Meta["reference_url"] != nil {
		t.Fatalf("error result must not carry a link: %v", res.Meta)
	}
}

func TestGetDatabaseServerMetricsHandler_DatabasesLink(t *testing.T) {
	server := staticServer(t, http.StatusOK, `[]`)
	handler := NewGetDatabaseServerMetricsHandler(server.Client(), linkTestConfig(server.URL))

	res, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetDatabaseServerMetricsArgs{DBSystem: "oracle", LookbackMinutes: 60})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if got := referenceURL(t, res).String(); got != "/v2/organizations/acme/databases" {
		t.Fatalf("reference_url = %q", got)
	}
}

func TestGetDatabaseServerMetricsHandler_NoLinkOnError(t *testing.T) {
	handler := NewGetDatabaseServerMetricsHandler(nil, linkTestConfig("http://unused"))
	res, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetDatabaseServerMetricsArgs{DBSystem: "cassandra"})
	if err == nil || res != nil {
		t.Fatalf("expected an error and no result, got %+v, %v", res, err)
	}
}

func TestGetServiceProfileHandler_ServiceLink(t *testing.T) {
	useFakeNow(t, "2026-02-09T16:04:05Z")
	server := staticServer(t, http.StatusOK, `{"service":"checkout"}`)
	handler := NewGetServiceProfileHandler(server.Client(), linkTestConfig(server.URL))

	res, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetServiceProfileArgs{ServiceName: "checkout"})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	u := referenceURL(t, res)
	if u.Path != "/v2/organizations/acme/service-catalog/checkout" {
		t.Fatalf("path = %q", u.Path)
	}
	assertQueryParams(t, u, map[string]string{
		"from":    "1770649445",
		"to":      "1770653045",
		"cluster": "cluster-1",
		"live":    "true",
	})
}

func TestGetServiceProfileHandler_NoLinkOnUpstreamError(t *testing.T) {
	server := staticServer(t, http.StatusInternalServerError, `{"error":"boom"}`)
	handler := NewGetServiceProfileHandler(server.Client(), linkTestConfig(server.URL))

	res, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetServiceProfileArgs{ServiceName: "checkout"})
	if err == nil {
		t.Fatalf("expected an error, got %+v", res)
	}
	if res != nil && res.Meta["reference_url"] != nil {
		t.Fatalf("error result must not carry a link: %v", res.Meta)
	}
}
