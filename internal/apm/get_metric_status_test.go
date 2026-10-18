package apm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestGetMetricStatus_AbsentMetric(t *testing.T) {
	var queries []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload struct {
			Query string `json:"query"`
		}
		_ = json.Unmarshal(body, &payload)
		queries = append(queries, payload.Query)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	cfg := models.Config{APIBaseURL: server.URL, Region: "us-east-1"}
	cfg.TokenManager = &auth.TokenManager{
		AccessToken: "mock-access-token",
		ExpiresAt:   time.Now().Add(time.Hour),
	}

	handler := NewGetMetricStatusHandler(server.Client(), cfg)
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetMetricStatusArgs{
		Metric:        "metric_that_does_not_exist",
		WindowMinutes: 60,
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if result == nil || len(result.Content) == 0 {
		t.Fatal("expected result content")
	}
	text := result.Content[0].(*mcp.TextContent).Text

	var status GetMetricStatusResult
	if err := json.Unmarshal([]byte(text), &status); err != nil {
		t.Fatalf("unmarshal: %v\nbody: %s", err, text)
	}
	if status.Live {
		t.Fatal("expected live=false for absent metric")
	}
	if status.SeriesCount != 0 {
		t.Fatalf("series_count=%d, want 0", status.SeriesCount)
	}
	if len(queries) == 0 {
		t.Fatal("expected at least one PromQL request")
	}
	if !strings.Contains(queries[0], "count(last_over_time(") {
		t.Fatalf("first query should use count(last_over_time(...)); got %q", queries[0])
	}
}

func TestGetMetricStatus_DailyMetricSuggestedWindow(t *testing.T) {
	now := time.Now().UTC().Unix()
	var instantN int
	var queries []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload struct {
			Query string `json:"query"`
		}
		_ = json.Unmarshal(body, &payload)
		queries = append(queries, payload.Query)

		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "/prom_query_instant"):
			instantN++
			if strings.Contains(payload.Query, "count(last_over_time(") {
				_, _ = w.Write([]byte(fmt.Sprintf(`[{"metric":{},"value":[%d,"3"]}]`, now)))
				return
			}
			if strings.Contains(payload.Query, "time() - timestamp(") {
				_, _ = w.Write([]byte(fmt.Sprintf(`[{"metric":{},"value":[%d,"86400"]}]`, now)))
				return
			}
			if strings.Contains(payload.Query, "scrape_interval(") {
				_, _ = w.Write([]byte(fmt.Sprintf(`[{"metric":{},"value":[%d,"86400"]}]`, now)))
				return
			}
			t.Fatalf("unexpected instant query: %s", payload.Query)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	cfg := models.Config{APIBaseURL: server.URL, Region: "us-east-1"}
	cfg.TokenManager = &auth.TokenManager{
		AccessToken: "mock-access-token",
		ExpiresAt:   time.Now().Add(time.Hour),
	}

	handler := NewGetMetricStatusHandler(server.Client(), cfg)
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetMetricStatusArgs{
		Metric:        "amazonaws_com_AWS_S3_BucketSizeBytes",
		WindowMinutes: 5760,
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	text := result.Content[0].(*mcp.TextContent).Text

	var status GetMetricStatusResult
	if err := json.Unmarshal([]byte(text), &status); err != nil {
		t.Fatalf("unmarshal: %v\nbody: %s", err, text)
	}
	if !status.Live {
		t.Fatal("expected live=true")
	}
	if status.SeriesCount != 3 {
		t.Fatalf("series_count=%d, want 3", status.SeriesCount)
	}
	if status.LastSampleAgeSeconds < 85000 || status.LastSampleAgeSeconds > 90000 {
		t.Fatalf("last_sample_age_seconds=%v, want ~86400", status.LastSampleAgeSeconds)
	}
	if status.InferredIntervalSeconds < 85000 || status.InferredIntervalSeconds > 90000 {
		t.Fatalf("inferred_interval_seconds=%v, want ~86400", status.InferredIntervalSeconds)
	}
	if status.SuggestedWindow != "3d" {
		t.Fatalf("suggested_window=%q, want %q", status.SuggestedWindow, "3d")
	}
	if instantN != 3 {
		t.Fatalf("expected 3 instant calls; got %d", instantN)
	}
	joined := strings.Join(queries, "\n")
	if strings.Contains(joined, "timestamp(last_over_time(") {
		t.Fatalf("must not use timestamp(last_over_time(...)) anti-pattern; queries:\n%s", joined)
	}
	if !strings.Contains(joined, "time() - timestamp(") {
		t.Fatalf("expected time() - timestamp(<metric>[window]); queries:\n%s", joined)
	}
}

func TestGetMetricStatus_LiveHighCadenceUsesInterval(t *testing.T) {
	now := time.Now().UTC().Unix()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload struct {
			Query string `json:"query"`
		}
		_ = json.Unmarshal(body, &payload)
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/prom_query_instant") {
			if strings.Contains(payload.Query, "count(last_over_time(") {
				_, _ = w.Write([]byte(fmt.Sprintf(`[{"metric":{},"value":[%d,"1"]}]`, now)))
				return
			}
			_, _ = w.Write([]byte(fmt.Sprintf(`[{"metric":{},"value":[%d,"15"]}]`, now)))
			return
		}
		t.Errorf("unexpected non-instant request: %s", r.URL.Path)
	}))
	defer server.Close()

	cfg := models.Config{APIBaseURL: server.URL, Region: "us-east-1"}
	cfg.TokenManager = &auth.TokenManager{
		AccessToken: "mock-access-token",
		ExpiresAt:   time.Now().Add(time.Hour),
	}

	handler := NewGetMetricStatusHandler(server.Client(), cfg)
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetMetricStatusArgs{
		Metric:        "up",
		WindowMinutes: 15,
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	var status GetMetricStatusResult
	if err := json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &status); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !status.Live || status.SuggestedWindow != "$__interval" {
		t.Fatalf("got live=%v suggested_window=%q; want live with $__interval", status.Live, status.SuggestedWindow)
	}
	if status.InferredIntervalSeconds != 15 {
		t.Fatalf("inferred_interval_seconds=%v, want 15", status.InferredIntervalSeconds)
	}
}

func TestGetMetricStatus_RequiresMetricAndWindow(t *testing.T) {
	cfg := models.Config{APIBaseURL: "http://example.invalid", Region: "us-east-1"}
	cfg.TokenManager = &auth.TokenManager{
		AccessToken: "mock-access-token",
		ExpiresAt:   time.Now().Add(time.Hour),
	}
	handler := NewGetMetricStatusHandler(http.DefaultClient, cfg)

	if _, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetMetricStatusArgs{WindowMinutes: 60}); err == nil {
		t.Fatal("expected error for empty metric")
	}
	if _, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetMetricStatusArgs{Metric: "up"}); err == nil {
		t.Fatal("expected error for missing window_minutes")
	}
}

func TestGetMetricStatus_NamedDatasourceRouting(t *testing.T) {
	var requests []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		requests = append(requests, payload)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	cfg := models.Config{
		APIBaseURL:         server.URL,
		Region:             "us-east-1",
		PrometheusReadURL:  "https://default.example/read",
		PrometheusUsername: "default-user",
		PrometheusPassword: "default-pass",
		Datasources: []models.DatasourceInfo{{
			Name: "secondary", ReadURL: "https://secondary.example/read",
			Username: "secondary-user", Password: "secondary-pass", Region: "eu-west-1",
		}},
	}
	cfg.TokenManager = &auth.TokenManager{AccessToken: "mock-access-token", ExpiresAt: time.Now().Add(time.Hour)}

	if _, _, err := NewGetMetricStatusHandler(server.Client(), cfg)(context.Background(), &mcp.CallToolRequest{}, GetMetricStatusArgs{Metric: "up", WindowMinutes: 60, Datasource: "secondary"}); err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if len(requests) != 1 {
		t.Fatalf("got %d requests, want 1", len(requests))
	}
	for field, want := range map[string]string{"read_url": "https://secondary.example/read", "username": "secondary-user", "password": "secondary-pass"} {
		if got := requests[0][field]; got != want {
			t.Errorf("%s=%v, want %q", field, got, want)
		}
	}

	_, _, err := NewGetMetricStatusHandler(server.Client(), cfg)(context.Background(), &mcp.CallToolRequest{}, GetMetricStatusArgs{Metric: "up", WindowMinutes: 60, Datasource: "missing"})
	if err == nil || !strings.Contains(err.Error(), `datasource "missing" not found`) {
		t.Fatalf("unknown datasource: err=%v", err)
	}
	if len(requests) != 1 {
		t.Fatalf("unknown datasource must not issue a request; got %d", len(requests))
	}
}

func TestGetMetricStatus_UpstreamErrorFailsTool(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()

	cfg := models.Config{APIBaseURL: server.URL, Region: "us-east-1"}
	cfg.TokenManager = &auth.TokenManager{AccessToken: "mock-access-token", ExpiresAt: time.Now().Add(time.Hour)}

	_, _, err := NewGetMetricStatusHandler(server.Client(), cfg)(context.Background(), &mcp.CallToolRequest{}, GetMetricStatusArgs{Metric: "up", WindowMinutes: 60})
	if err == nil || !strings.Contains(err.Error(), "502") {
		t.Fatalf("err=%v, want an error naming the 502", err)
	}
}

func TestGetMetricStatus_DropsNonFiniteSamples(t *testing.T) {
	now := time.Now().UTC().Unix()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Query string `json:"query"`
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(payload.Query, "count(last_over_time("):
			_, _ = fmt.Fprintf(w, `[{"metric":{},"value":[%d,"2"]},{"metric":{},"value":[%d,"NaN"]}]`, now, now)
		case strings.Contains(payload.Query, "time() - timestamp("):
			_, _ = fmt.Fprintf(w, `[{"metric":{},"value":[%d,"-Inf"]},{"metric":{},"value":[%d,"30"]}]`, now, now)
		default:
			_, _ = fmt.Fprintf(w, `[{"metric":{},"value":[%d,"+Inf"]},{"metric":{},"value":[%d,"60"]}]`, now, now)
		}
	}))
	defer server.Close()

	cfg := models.Config{APIBaseURL: server.URL, Region: "us-east-1"}
	cfg.TokenManager = &auth.TokenManager{AccessToken: "mock-access-token", ExpiresAt: time.Now().Add(time.Hour)}

	result, _, err := NewGetMetricStatusHandler(server.Client(), cfg)(context.Background(), &mcp.CallToolRequest{}, GetMetricStatusArgs{Metric: "up", WindowMinutes: 60})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	var status GetMetricStatusResult
	if err := json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &status); err != nil {
		t.Fatal(err)
	}
	if status.SeriesCount != 2 || status.LastSampleAgeSeconds != 30 || status.InferredIntervalSeconds != 60 {
		t.Fatalf("got %+v; want series_count=2 age=30 interval=60", status)
	}
}

func TestFormatSuggestedDuration(t *testing.T) {
	if got := formatSuggestedDuration(3 * 86400); got != "3d" {
		t.Fatalf("got %q, want 3d", got)
	}
	if got := formatSuggestedDuration(2.1 * 86400); got != "3d" {
		t.Fatalf("ceil days: got %q, want 3d", got)
	}
}

func TestSuggestWindow_SlowEmitterSoonAfterSample(t *testing.T) {
	// Daily cadence with a fresh sample must still suggest ~3d, not $__interval.
	if got := suggestWindow(10, 86400); got != "3d" {
		t.Fatalf("suggestWindow(10, 86400)=%q, want 3d", got)
	}
	if got := suggestWindow(400, 86400); got != "3d" {
		t.Fatalf("suggestWindow(400, 86400)=%q, want 3d", got)
	}
	if got := suggestWindow(15, 15); got != "$__interval" {
		t.Fatalf("suggestWindow(15, 15)=%q, want $__interval", got)
	}
	if got := suggestWindow(10, 0); got != "$__interval" {
		t.Fatalf("unknown cadence + fresh sample: got %q, want $__interval", got)
	}
}

func TestGetMetricStatus_IntervalIndependentOfWindow(t *testing.T) {
	now := time.Now().UTC().Unix()
	for _, minutes := range []float64{60, 5760} {
		var intervalQuery string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var payload struct {
				Query string `json:"query"`
			}
			_ = json.NewDecoder(r.Body).Decode(&payload)
			if !strings.Contains(r.URL.Path, "/prom_query_instant") {
				t.Errorf("unexpected non-instant request: %s", r.URL.Path)
			}
			val := "0"
			switch {
			case strings.Contains(payload.Query, "count(last_over_time("):
				val = "1"
			case strings.Contains(payload.Query, "scrape_interval("):
				intervalQuery = payload.Query
				val = "60"
			}
			_, _ = w.Write([]byte(fmt.Sprintf(`[{"metric":{},"value":[%d,%q]}]`, now, val)))
		}))
		cfg := models.Config{APIBaseURL: server.URL, Region: "us-east-1"}
		cfg.TokenManager = &auth.TokenManager{AccessToken: "mock-access-token", ExpiresAt: time.Now().Add(time.Hour)}
		result, _, err := NewGetMetricStatusHandler(server.Client(), cfg)(context.Background(), &mcp.CallToolRequest{}, GetMetricStatusArgs{Metric: "up", WindowMinutes: minutes})
		server.Close()
		if err != nil {
			t.Fatal(err)
		}
		var status GetMetricStatusResult
		if err := json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &status); err != nil {
			t.Fatal(err)
		}
		if status.InferredIntervalSeconds != 60 || status.SuggestedWindow != "$__interval" {
			t.Fatalf("window=%vm: interval=%v window=%q; want 60 and $__interval", minutes, status.InferredIntervalSeconds, status.SuggestedWindow)
		}
		if !strings.HasPrefix(intervalQuery, "scrape_interval(") {
			t.Fatalf("cadence must come from a raw-sample rollup, got %q", intervalQuery)
		}
	}
}
