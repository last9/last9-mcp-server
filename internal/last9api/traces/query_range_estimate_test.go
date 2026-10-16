package traces

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

func testClient(t *testing.T, h http.HandlerFunc) *last9api.Client {
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
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		query = r.URL.Query()
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = w.Write([]byte(`{"data":{"bytes":4096,"rows":3,"scoped":false}}`))
	})

	got, err := EstimateQueryRange(context.Background(), c, EstimateQueryRangeInput{
		Pipeline: []map[string]any{{"type": "filter"}}, StartMs: 1_700_000_000_000, EndMs: 1_700_003_600_000,
	})
	if err != nil {
		t.Fatalf("EstimateQueryRange: %v", err)
	}
	if got != (last9api.QueryEstimate{Bytes: 4096, Rows: 3}) {
		t.Errorf("estimate = %+v", got)
	}
	if path != "/cat/api/traces/v2/query_range/json/estimate" {
		t.Errorf("path = %q", path)
	}
	want := map[string]string{"start": "1700000000", "end": "1700003600", "region": "ap-south-1", "direction": "backward"}
	for k, v := range want {
		if got := strings.Join(query[k], ","); got != v {
			t.Errorf("query %s = %q, want %q", k, got, v)
		}
	}
	if _, ok := body["pipeline"].([]any); !ok {
		t.Errorf("body pipeline = %v", body["pipeline"])
	}
}

func TestEstimateQueryRange_Errors(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
	}{
		{"bad request", http.StatusBadRequest, `{"error":"bad pipeline"}`},
		{"server error", http.StatusBadGateway, ``},
		{"no bytes", http.StatusOK, `{"data":{}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
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
