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

func TestMetricStatus_AbsentMetric(t *testing.T) {
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

	handler := NewMetricStatusHandler(server.Client(), cfg)
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, MetricStatusArgs{
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

	var status MetricStatusResult
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

func TestMetricStatus_DailyMetricSuggestedWindow(t *testing.T) {
	now := time.Now().UTC().Unix()
	dayAgo := now - 86400
	var instantN, rangeN int
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
			t.Fatalf("unexpected instant query: %s", payload.Query)
		case strings.Contains(r.URL.Path, "/prom_query"):
			rangeN++
			// Three daily samples → median delta ≈ 86400s
			_, _ = w.Write([]byte(fmt.Sprintf(
				`[{"metric":{"bucket":"a"},"values":[[%d,"1"],[%d,"1"],[%d,"1"]]}]`,
				dayAgo-86400, dayAgo, now,
			)))
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

	handler := NewMetricStatusHandler(server.Client(), cfg)
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, MetricStatusArgs{
		Metric:        "amazonaws_com_AWS_S3_BucketSizeBytes",
		WindowMinutes: 5760,
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	text := result.Content[0].(*mcp.TextContent).Text

	var status MetricStatusResult
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
	if instantN != 2 || rangeN != 1 {
		t.Fatalf("expected 2 instant + 1 range calls; got instant=%d range=%d", instantN, rangeN)
	}
	joined := strings.Join(queries, "\n")
	if strings.Contains(joined, "timestamp(last_over_time(") {
		t.Fatalf("must not use timestamp(last_over_time(...)) anti-pattern; queries:\n%s", joined)
	}
	if !strings.Contains(joined, "time() - timestamp(") {
		t.Fatalf("expected time() - timestamp(<metric>[window]); queries:\n%s", joined)
	}
}

func TestMetricStatus_LiveHighCadenceUsesInterval(t *testing.T) {
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
		_, _ = w.Write([]byte(fmt.Sprintf(
			`[{"metric":{},"values":[[%d,"1"],[%d,"1"],[%d,"1"],[%d,"1"]]}]`,
			now-45, now-30, now-15, now,
		)))
	}))
	defer server.Close()

	cfg := models.Config{APIBaseURL: server.URL, Region: "us-east-1"}
	cfg.TokenManager = &auth.TokenManager{
		AccessToken: "mock-access-token",
		ExpiresAt:   time.Now().Add(time.Hour),
	}

	handler := NewMetricStatusHandler(server.Client(), cfg)
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, MetricStatusArgs{
		Metric:        "up",
		WindowMinutes: 15,
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	var status MetricStatusResult
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

func TestMetricStatus_RequiresMetricAndWindow(t *testing.T) {
	cfg := models.Config{APIBaseURL: "http://example.invalid", Region: "us-east-1"}
	cfg.TokenManager = &auth.TokenManager{
		AccessToken: "mock-access-token",
		ExpiresAt:   time.Now().Add(time.Hour),
	}
	handler := NewMetricStatusHandler(http.DefaultClient, cfg)

	if _, _, err := handler(context.Background(), &mcp.CallToolRequest{}, MetricStatusArgs{WindowMinutes: 60}); err == nil {
		t.Fatal("expected error for empty metric")
	}
	if _, _, err := handler(context.Background(), &mcp.CallToolRequest{}, MetricStatusArgs{Metric: "up"}); err == nil {
		t.Fatal("expected error for missing window_minutes")
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
