package logs

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
	"last9-mcp/internal/last9api"
	"last9-mcp/internal/models"
)

func estimateTestClient(t *testing.T, h http.HandlerFunc) *last9api.Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return last9api.NewClient(srv.Client(), models.Config{
		APIBaseURL: srv.URL,
		Region:     "ap-south-1",
		TokenManager: &auth.TokenManager{
			AccessToken: "mock-token",
			ExpiresAt:   time.Now().Add(time.Hour),
		},
	})
}

func TestEstimateQueryRange_RequestShape(t *testing.T) {
	var path string
	var query map[string][]string
	var body map[string]any
	c := estimateTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		query = r.URL.Query()
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = w.Write([]byte(`{"data":{"bytes":2048,"rows":10,"parts":1,"marks":2,"start":1,"end":2,"scoped":true}}`))
	})

	pipeline := []map[string]any{{"type": "filter", "query": map[string]any{"$eq": []any{"ServiceName", "api"}}}}
	got, err := EstimateQueryRange(context.Background(), c, EstimateQueryRangeInput{
		Pipeline: pipeline, StartMs: 1_700_000_000_000, EndMs: 1_700_003_600_000, Index: "physical_index:payments",
	})
	if err != nil {
		t.Fatalf("EstimateQueryRange: %v", err)
	}
	if got != (last9api.QueryEstimate{Bytes: 2048, Rows: 10, Scoped: true}) {
		t.Errorf("estimate = %+v", got)
	}
	if path != "/logs/api/v2/query_range/json/estimate" {
		t.Errorf("path = %q", path)
	}
	want := map[string]string{"start": "1700000000", "end": "1700003600", "region": "ap-south-1", "direction": "backward", "index": "physical_index:payments"}
	for k, v := range want {
		if got := strings.Join(query[k], ","); got != v {
			t.Errorf("query %s = %q, want %q", k, got, v)
		}
	}
	if _, ok := body["pipeline"].([]any); !ok {
		t.Errorf("body pipeline = %v", body["pipeline"])
	}
}

func TestEstimateQueryRange_OmitsEmptyIndex(t *testing.T) {
	var query map[string][]string
	c := estimateTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query()
		_, _ = io.Copy(io.Discard, r.Body)
		_, _ = w.Write([]byte(`{"data":{"bytes":0}}`))
	})
	if _, err := EstimateQueryRange(context.Background(), c, EstimateQueryRangeInput{StartMs: 0, EndMs: 1000}); err != nil {
		t.Fatalf("EstimateQueryRange: %v", err)
	}
	if _, ok := query["index"]; ok {
		t.Errorf("index sent for an empty index: %v", query["index"])
	}
}

func TestEstimateQueryRange_Errors(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
	}{
		{"bad request", http.StatusBadRequest, `{"error":"bad pipeline"}`},
		{"not found", http.StatusNotFound, `404 page not found`},
		{"no data", http.StatusOK, `{}`},
		{"no bytes", http.StatusOK, `{"data":{"rows":5}}`},
		{"null bytes", http.StatusOK, `{"data":{"bytes":null}}`},
		{"not json", http.StatusOK, `<html>`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := estimateTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			})
			if _, err := EstimateQueryRange(context.Background(), c, EstimateQueryRangeInput{EndMs: 1000}); err == nil {
				t.Fatal("want an error")
			}
		})
	}
}

func TestEstimateQueryRange_RejectsInvalidIndexBeforeCalling(t *testing.T) {
	calls := 0
	c := estimateTestClient(t, func(w http.ResponseWriter, r *http.Request) { calls++ })
	if _, err := EstimateQueryRange(context.Background(), c, EstimateQueryRangeInput{Index: "bogus"}); err == nil {
		t.Fatal("want an error for an invalid index")
	}
	if calls != 0 {
		t.Errorf("calls = %d, want 0", calls)
	}
}
