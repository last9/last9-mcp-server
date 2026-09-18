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

// Serves rowCount aggregate rows plus the PromQL baseline AppendCountSanity
// fetches. The baseline call count is the observable: sanity must not run on a
// result the row cap already trimmed.
func aggregateSearchServer(t *testing.T, rowCount int) (*httptest.Server, *int) {
	t.Helper()
	rows := make([]map[string]any, 0, rowCount)
	for i := 0; i < rowCount; i++ {
		rows = append(rows, map[string]any{
			"metric": map[string]any{"svc": "checkout", "c": 3},
			"values": []any{},
		})
	}
	body, err := json.Marshal(map[string]any{
		"query_result": map[string]any{
			"status": "success",
			"data":   map[string]any{"resultType": "matrix", "result": rows},
		},
		"search_stats": map[string]any{"strategy": "direct"},
	})
	if err != nil {
		t.Fatalf("marshal aggregate body: %v", err)
	}

	var mu sync.Mutex
	baselineCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "prom_query_instant") {
			mu.Lock()
			baselineCalls++
			mu.Unlock()
			_, _ = w.Write([]byte(`[{"metric":{},"value":[1700000000,"1000"]}]`))
			return
		}
		_, _ = w.Write(body)
	}))
	return srv, &baselineCalls
}

// countAggregatePipeline is the shape AppendCountSanity acts on: a single
// scoped service plus a $count aggregate it can total.
func countAggregatePipeline() []map[string]interface{} {
	return []map[string]interface{}{
		{"type": "filter", "query": map[string]interface{}{
			"$eq": []interface{}{"ServiceName", "checkout"},
		}},
		{"type": "aggregate", "aggregates": []interface{}{
			map[string]interface{}{
				"function": map[string]interface{}{"$count": []interface{}{}},
				"as":       "c",
			},
		}},
	}
}

// The row cap is applied to the decoded result on this path — the endpoint 400s
// on a limit for an aggregate, so it cannot be requested upfront. This is the
// behaviour the chunked path gained in #227 and the divergence the merge risked.
func TestFetchLogJSONQuery_APIMode_AggregateRowCap(t *testing.T) {
	srv, baselineCalls := aggregateSearchServer(t, 12)
	defer srv.Close()

	result, err := fetchLogJSONQuery(
		context.Background(), srv.Client(), apiModeCfg(srv.URL), countAggregatePipeline(),
		1_700_000_000_000, 1_700_000_600_000, GetLogsArgs{Limit: 10})
	if err != nil {
		t.Fatalf("fetchLogJSONQuery: %v", err)
	}

	data := result["data"].(map[string]any)
	if got := len(data["result"].([]interface{})); got != 10 {
		t.Errorf("rows = %d, want 10 (trimmed to the requested cap)", got)
	}

	marker, ok := result["l9_result"].(map[string]interface{})
	if !ok {
		t.Fatalf("l9_result = %#v, want the partial marker", result["l9_result"])
	}
	for key, want := range map[string]interface{}{
		"partial": true, "reason": "row_limit_reached",
		"returned_rows": 10, "row_limit": 10,
	} {
		if marker[key] != want {
			t.Errorf("l9_result[%q] = %v, want %v", key, marker[key], want)
		}
	}

	// A trimmed result is not a count of anything, so sanity must not run on it.
	if _, present := result["l9_sanity"]; present {
		t.Error("l9_sanity must be omitted when the row cap trimmed the result")
	}
	if *baselineCalls != 0 {
		t.Errorf("baseline fetched %d times on a trimmed result, want 0", *baselineCalls)
	}
}

// The other side of the same branch: under the cap nothing is trimmed, no
// marker is attached, and the sanity block still runs as it does today.
func TestFetchLogJSONQuery_APIMode_AggregateUnderCapKeepsSanity(t *testing.T) {
	srv, baselineCalls := aggregateSearchServer(t, 3)
	defer srv.Close()

	result, err := fetchLogJSONQuery(
		context.Background(), srv.Client(), apiModeCfg(srv.URL), countAggregatePipeline(),
		1_700_000_000_000, 1_700_000_600_000, GetLogsArgs{Limit: 10})
	if err != nil {
		t.Fatalf("fetchLogJSONQuery: %v", err)
	}

	data := result["data"].(map[string]any)
	if got := len(data["result"].([]interface{})); got != 3 {
		t.Errorf("rows = %d, want all 3 kept", got)
	}
	if _, present := result["l9_result"]; present {
		t.Errorf("l9_result = %#v, want no partial marker under the cap", result["l9_result"])
	}
	if _, present := result["l9_sanity"]; !present {
		t.Error("l9_sanity must still be attached when nothing was trimmed")
	}
	if *baselineCalls == 0 {
		t.Error("baseline was never fetched, so the sanity path did not run")
	}
}
