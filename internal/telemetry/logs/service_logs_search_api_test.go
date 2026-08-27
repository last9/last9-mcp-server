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
)

func serviceLogsPipeline() []map[string]interface{} {
	return buildServiceLogsQuery("checkout", []string{"error"}, nil)
}

func serviceLogsWindow() (time.Time, time.Time) {
	end := time.Unix(1_700_000_600, 0).UTC()
	return end.Add(-10 * time.Minute), end
}

// The whole window is answered by one call, and the extras the endpoint returns
// reach the tool's response instead of being dropped on the floor.
func TestFetchServiceLogs_APIMode_SingleCallWithCoverage(t *testing.T) {
	var mu sync.Mutex
	var body map[string]any
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		paths = append(paths, r.URL.Path)
		_ = json.Unmarshal(raw, &body)
		mu.Unlock()
		_, _ = w.Write([]byte(`{
			"query_result":{"status":"success","data":{"resultType":"streams","result":[
				{"stream":{"severity":"ERROR"},"values":[["1700000000000000000","boom"]]}
			]}},
			"total_matching_lines": 41,
			"search_stats":{"chunks_planned":6,"chunks_failed":1}
		}`))
	}))
	defer srv.Close()

	start, end := serviceLogsWindow()
	got, err := fetchServiceLogs(
		context.Background(), srv.Client(), apiModeCfg(srv.URL), "checkout",
		start, end, 20, serviceLogsPipeline(), "")
	if err != nil {
		t.Fatalf("fetchServiceLogs: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	// The point of the change: one request, not one per chunk.
	if len(paths) != 1 || paths[0] != "/logs/query" {
		t.Errorf("requests = %v, want a single /logs/query", paths)
	}
	if got := body["limit"]; got != float64(20) {
		t.Errorf("limit = %v, want 20", got)
	}
	// Seconds on the wire, milliseconds in the caller.
	if got := body["start"]; got != float64(start.Unix()) {
		t.Errorf("start = %v, want %d seconds", got, start.Unix())
	}

	if got.Count != 1 || len(got.Logs) != 1 {
		t.Fatalf("logs = %#v, want exactly one entry", got.Logs)
	}
	if got.Logs[0].Message != "boom" || got.Logs[0].Severity != "ERROR" {
		t.Errorf("entry = %#v, want the parsed message and severity", got.Logs[0])
	}
	if got.TotalMatchingLines == nil || *got.TotalMatchingLines != 41 {
		t.Errorf("total_matching_lines = %v, want 41", got.TotalMatchingLines)
	}
	if got.SearchStats["chunks_failed"] != float64(1) {
		t.Errorf("search_stats = %#v, want chunks_failed carried through", got.SearchStats)
	}
}

// With no coverage in the response the fields stay absent, not zero: a count of
// 0 means nothing matched, absent means nobody counted.
func TestFetchServiceLogs_APIMode_OmitsAbsentCoverage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"query_result":{"status":"success","data":{"resultType":"streams","result":[]}}}`))
	}))
	defer srv.Close()

	start, end := serviceLogsWindow()
	got, err := fetchServiceLogs(
		context.Background(), srv.Client(), apiModeCfg(srv.URL), "checkout",
		start, end, 20, serviceLogsPipeline(), "")
	if err != nil {
		t.Fatalf("fetchServiceLogs: %v", err)
	}
	if got.TotalMatchingLines != nil {
		t.Errorf("total_matching_lines = %v, want absent", *got.TotalMatchingLines)
	}

	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, key := range []string{"total_matching_lines", "search_stats"} {
		if strings.Contains(string(encoded), key) {
			t.Errorf("%q must be omitted from the payload, got %s", key, encoded)
		}
	}
}

// Hard fail, no fallback — the same rule get_logs follows, so the two paths
// stay separable and the upstream's own message reaches the model.
func TestFetchServiceLogs_APIMode_SurfacesUpstreamMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"limit must be positive"}`))
	}))
	defer srv.Close()

	start, end := serviceLogsWindow()
	_, err := fetchServiceLogs(
		context.Background(), srv.Client(), apiModeCfg(srv.URL), "checkout",
		start, end, 20, serviceLogsPipeline(), "")
	if err == nil {
		t.Fatal("a 400 must be returned, never retried through the chunked path")
	}
	if !strings.Contains(err.Error(), "limit must be positive") {
		t.Errorf("error = %q, want the upstream message preserved", err)
	}
}

func TestFetchServiceLogs_FlagOffUsesChunkedPath(t *testing.T) {
	var mu sync.Mutex
	var sawSearch bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		sawSearch = sawSearch || r.URL.Path == "/logs/query"
		mu.Unlock()
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"streams","result":[]}}`))
	}))
	defer srv.Close()

	cfg := apiModeCfg(srv.URL)
	cfg.UseLogSearchAPI = false

	start, end := serviceLogsWindow()
	if _, err := fetchServiceLogs(
		context.Background(), srv.Client(), cfg, "checkout",
		start, end, 20, serviceLogsPipeline(), ""); err != nil {
		t.Fatalf("fetchServiceLogs: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if sawSearch {
		t.Error("with the flag off the chunked path must be used")
	}
}
