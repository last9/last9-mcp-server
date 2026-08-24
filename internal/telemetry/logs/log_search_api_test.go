package logs

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/models"
)

func apiModeCfg(apiBaseURL string) models.Config {
	return models.Config{
		APIBaseURL:        apiBaseURL,
		Region:            "us-east-1",
		UseLogSearchAPI:   true,
		MaxGetLogsEntries: models.DefaultMaxGetLogsEntries,
		TokenManager: &auth.TokenManager{
			AccessToken: "test-token",
			ExpiresAt:   time.Now().Add(time.Hour),
		},
	}
}

func TestFetchLogJSONQuery_APIMode_RawSendsLimit(t *testing.T) {
	var body map[string]any
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		_, _ = w.Write([]byte(`{
			"query_result":{"status":"success","data":{"resultType":"streams","result":[]}},
			"total_matching_lines": 7,
			"search_stats":{"bucket_seconds":60}
		}`))
	}))
	defer srv.Close()

	pipeline := []map[string]interface{}{{"type": "filter"}}
	result, err := fetchLogJSONQuery(
		context.Background(), srv.Client(), apiModeCfg(srv.URL), pipeline,
		1_700_000_000_000, 1_700_000_600_000, GetLogsArgs{Limit: 100})
	if err != nil {
		t.Fatalf("fetchLogJSONQuery: %v", err)
	}

	if path != "/logs/query" {
		t.Errorf("path = %q, want /logs/query — the flag did not route to the API", path)
	}
	if got := body["limit"]; got != float64(100) {
		t.Errorf("limit = %v, want 100", got)
	}
	if result["total_matching_lines"] != 7 {
		t.Errorf("total_matching_lines = %v, want 7", result["total_matching_lines"])
	}
	// The tool's output shape must not shift between modes.
	data, ok := result["data"].(map[string]any)
	if !ok || data["resultType"] != "streams" {
		t.Errorf("data = %#v, want a streams envelope at the top level", result["data"])
	}
}

func TestFetchLogJSONQuery_APIMode_AggregateOmitsLimit(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		_, _ = w.Write([]byte(`{
			"query_result":{"status":"success","data":{"resultType":"matrix","result":[]}},
			"search_stats":{"strategy":"direct"}
		}`))
	}))
	defer srv.Close()

	// A limit on an aggregate pipeline is a 400 upstream, so it must never be sent.
	pipeline := []map[string]interface{}{{"type": "aggregate"}}
	_, err := fetchLogJSONQuery(
		context.Background(), srv.Client(), apiModeCfg(srv.URL), pipeline,
		1_700_000_000_000, 1_700_000_600_000, GetLogsArgs{Limit: 100})
	if err != nil {
		t.Fatalf("fetchLogJSONQuery: %v", err)
	}
	if _, present := body["limit"]; present {
		t.Errorf("limit must be omitted for an aggregate pipeline, got %v", body["limit"])
	}
}

func TestFetchLogJSONQuery_APIMode_SurfacesUpstreamMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"index must be prefixed physical_index: or rehydration_index:"}`))
	}))
	defer srv.Close()

	pipeline := []map[string]interface{}{{"type": "filter"}}
	_, err := fetchLogJSONQuery(
		context.Background(), srv.Client(), apiModeCfg(srv.URL), pipeline,
		1_700_000_000_000, 1_700_000_600_000, GetLogsArgs{})
	if err == nil {
		t.Fatal("a 400 must be returned, never retried through the chunked path")
	}
	// The endpoint's own message is actionable, so it must reach the model.
	if !strings.Contains(err.Error(), "must be prefixed") {
		t.Errorf("error = %q, want the upstream message preserved", err)
	}
}

func TestFetchLogJSONQuery_FlagOffUsesChunkedPath(t *testing.T) {
	// The chunked path fires its chunks concurrently, so the handler runs on
	// several goroutines at once. Guard the recorded path or -race fails here.
	var mu sync.Mutex
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		path = r.URL.Path
		mu.Unlock()
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"streams","result":[]}}`))
	}))
	defer srv.Close()

	cfg := apiModeCfg(srv.URL)
	cfg.UseLogSearchAPI = false

	pipeline := []map[string]interface{}{{"type": "filter"}}
	if _, err := fetchLogJSONQuery(
		context.Background(), srv.Client(), cfg, pipeline,
		1_700_000_000_000, 1_700_000_600_000, GetLogsArgs{}); err != nil {
		t.Fatalf("fetchLogJSONQuery: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if path == "/logs/query" {
		t.Error("with the flag off the chunked path must be used")
	}
}
