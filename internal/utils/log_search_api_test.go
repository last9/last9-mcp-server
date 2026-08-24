package utils

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"last9-mcp/internal/models"
)

// captureLogSearchBody spins up a server that records the decoded request body
// and replies with a minimal valid search response.
func captureLogSearchBody(t *testing.T) (*httptest.Server, *map[string]any, *string) {
	t.Helper()
	var body map[string]any
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"query_result":{"status":"success"},"search_stats":{}}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &body, &path
}

func logSearchTestCfg(t *testing.T, apiBaseURL string) models.Config {
	t.Helper()
	cfg := sanityTestCfg(t, apiBaseURL)
	cfg.Region = "us-east-1"
	return cfg
}

func TestMakeLogSearchAPI_RawPipelineBody(t *testing.T) {
	srv, body, path := captureLogSearchBody(t)
	cfg := logSearchTestCfg(t, srv.URL)

	pipeline := []map[string]interface{}{{"type": "filter"}}
	resp, err := MakeLogSearchAPI(context.Background(), srv.Client(), cfg, LogSearchRequest{
		Pipeline: pipeline,
		StartMs:  1_700_000_000_000,
		EndMs:    1_700_000_600_000,
		Limit:    100,
		Index:    "physical_index:my_index",
	})
	if err != nil {
		t.Fatalf("MakeLogSearchAPI: %v", err)
	}
	defer resp.Body.Close()

	if *path != "/logs/query" {
		t.Errorf("path = %q, want /logs/query", *path)
	}
	// Seconds on the wire, milliseconds inside MCP.
	if got := (*body)["start"]; got != float64(1_700_000_000) {
		t.Errorf("start = %v, want 1700000000", got)
	}
	if got := (*body)["end"]; got != float64(1_700_000_600) {
		t.Errorf("end = %v, want 1700000600", got)
	}
	if got := (*body)["region"]; got != "us-east-1" {
		t.Errorf("region = %v, want us-east-1", got)
	}
	if got := (*body)["limit"]; got != float64(100) {
		t.Errorf("limit = %v, want 100", got)
	}
	if got := (*body)["index"]; got != "physical_index:my_index" {
		t.Errorf("index = %v, want physical_index:my_index", got)
	}
	if _, present := (*body)["direction"]; present {
		t.Error("direction must be omitted so the server applies its own default")
	}
}

func TestMakeLogSearchAPI_OmitsUnsetOptionalFields(t *testing.T) {
	srv, body, _ := captureLogSearchBody(t)
	cfg := logSearchTestCfg(t, srv.URL)

	resp, err := MakeLogSearchAPI(context.Background(), srv.Client(), cfg, LogSearchRequest{
		Pipeline: []map[string]interface{}{{"type": "aggregate"}},
		StartMs:  0,
		EndMs:    60_000,
	})
	if err != nil {
		t.Fatalf("MakeLogSearchAPI: %v", err)
	}
	defer resp.Body.Close()

	// A limit on an aggregate query is a 400 upstream, so an unset limit must
	// not reach the wire as 0.
	if _, present := (*body)["limit"]; present {
		t.Error("limit must be omitted when unset")
	}
	if _, present := (*body)["index"]; present {
		t.Error("index must be omitted when empty")
	}
}

func TestMakeLogSearchAPI_ClampsLimitToServerMax(t *testing.T) {
	srv, body, _ := captureLogSearchBody(t)
	cfg := logSearchTestCfg(t, srv.URL)

	resp, err := MakeLogSearchAPI(context.Background(), srv.Client(), cfg, LogSearchRequest{
		Pipeline: []map[string]interface{}{{"type": "filter"}},
		StartMs:  0,
		EndMs:    60_000,
		Limit:    10_000,
	})
	if err != nil {
		t.Fatalf("MakeLogSearchAPI: %v", err)
	}
	defer resp.Body.Close()

	if got := (*body)["limit"]; got != float64(LogSearchMaxSampleSize) {
		t.Errorf("limit = %v, want %d", got, LogSearchMaxSampleSize)
	}
}

func TestMakeLogSearchAPI_RejectsUnprefixedIndex(t *testing.T) {
	srv, _, _ := captureLogSearchBody(t)
	cfg := logSearchTestCfg(t, srv.URL)

	_, err := MakeLogSearchAPI(context.Background(), srv.Client(), cfg, LogSearchRequest{
		Pipeline: []map[string]interface{}{{"type": "filter"}},
		StartMs:  0,
		EndMs:    60_000,
		Index:    "my_index",
	})
	if err == nil {
		t.Fatal("a bare index name must be rejected before the request is sent")
	}
}

func TestMakeLogSearchAPI_ReturnsNon200ForCallerToSanitize(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"limit is not applicable to an aggregate or dataframe query"}`))
	}))
	defer srv.Close()
	cfg := logSearchTestCfg(t, srv.URL)

	resp, err := MakeLogSearchAPI(context.Background(), srv.Client(), cfg, LogSearchRequest{
		Pipeline: []map[string]interface{}{{"type": "filter"}},
		StartMs:  0,
		EndMs:    60_000,
	})
	if err != nil {
		t.Fatalf("400 must return the response so the caller can sanitize: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}
