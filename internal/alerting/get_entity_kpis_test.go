package alerting

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func executeGetEntityKPIs(t *testing.T, handler http.HandlerFunc, args GetEntityKPIsArgs) (string, *mcp.CallToolResult, error) {
	t.Helper()
	srv := httptest.NewServer(handler)
	defer srv.Close()
	cfg := models.Config{
		APIBaseURL: srv.URL,
		OrgSlug:    "last9",
		ClusterID:  "cluster-1",
		TokenManager: &auth.TokenManager{
			AccessToken: "test-access-token",
			ExpiresAt:   time.Now().Add(time.Hour),
		},
	}
	result, _, err := NewGetEntityKPIsHandler(srv.Client(), cfg)(context.Background(), &mcp.CallToolRequest{}, args)
	if err != nil {
		return "", result, err
	}
	return utils.GetTextContent(t, result), result, nil
}

func TestGetEntityKPIsHandler_MissingEntityID(t *testing.T) {
	text, result, err := executeGetEntityKPIs(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		http.NotFound(w, r)
	}, GetEntityKPIsArgs{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected IsError")
	}
	if !strings.Contains(text, "entity_id is required") {
		t.Fatalf("got %q", text)
	}
}

func TestGetEntityKPIsHandler_ListsKPIs(t *testing.T) {
	text, result, err := executeGetEntityKPIs(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/entities/entity-abc/kpis" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{
				"id":   "kpi-1",
				"name": "p99_latency",
				"definition": map[string]string{
					"query": "histogram_quantile(0.99, rate(http_request_duration_seconds_bucket[5m]))",
					"unit":  "seconds",
				},
			},
			{
				"id":   "kpi-2",
				"name": "error_rate",
				"definition": map[string]string{
					"query": `rate(http_requests_total{status=~"5.."}[5m])`,
					"unit":  "ratio",
				},
			},
		})
	}, GetEntityKPIsArgs{EntityID: "entity-abc"})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %s", text)
	}
	if !strings.Contains(text, "Found 2 KPIs") {
		t.Fatalf("expected count header, got:\n%s", text)
	}
	for _, want := range []string{"kpi-1", "p99_latency", "seconds", "kpi-2", "error_rate", "ratio"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in:\n%s", want, text)
		}
	}
	if !strings.Contains(text, "histogram_quantile") {
		t.Fatalf("missing PromQL in:\n%s", text)
	}
}

func TestGetEntityKPIsHandler_EmptyList(t *testing.T) {
	text, _, err := executeGetEntityKPIs(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[]`))
	}, GetEntityKPIsArgs{EntityID: "entity-empty"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "Found 0 KPIs") {
		t.Fatalf("got:\n%s", text)
	}
}
