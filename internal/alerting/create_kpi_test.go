package alerting

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
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func executeCreateKPI(t *testing.T, handler http.HandlerFunc, args CreateKPIArgs) (string, *mcp.CallToolResult, error) {
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
	result, _, err := NewCreateKPIHandler(srv.Client(), cfg)(context.Background(), &mcp.CallToolRequest{}, args)
	if err != nil {
		return "", result, err
	}
	return utils.GetTextContent(t, result), result, nil
}

func TestCreateKPIHandler_RequiresFields(t *testing.T) {
	tests := []struct {
		name string
		args CreateKPIArgs
		want string
	}{
		{"entity", CreateKPIArgs{Name: "n", Query: "up", Unit: "ratio"}, "entity_id is required"},
		{"name", CreateKPIArgs{EntityID: "e", Query: "up", Unit: "ratio"}, "name is required"},
		{"query", CreateKPIArgs{EntityID: "e", Name: "n", Unit: "ratio"}, "query is required"},
		{"unit", CreateKPIArgs{EntityID: "e", Name: "n", Query: "up"}, "unit is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			text, result, err := executeCreateKPI(t, func(w http.ResponseWriter, r *http.Request) {
				t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
				http.NotFound(w, r)
			}, tt.args)
			if err != nil {
				t.Fatal(err)
			}
			if !result.IsError {
				t.Fatal("expected IsError")
			}
			if !strings.Contains(text, tt.want) {
				t.Fatalf("got %q want substring %q", text, tt.want)
			}
		})
	}
}

func TestCreateKPIHandler_CreatesAndReturnsID(t *testing.T) {
	text, result, err := executeCreateKPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s", r.Method)
		}
		if r.URL.Path != "/entities/entity-abc/kpis/" {
			t.Errorf("path = %s (trailing slash required)", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		var got map[string]any
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatal(err)
		}
		if got["name"] != "p99_latency" {
			t.Errorf("name = %v", got["name"])
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":   "kpi-new",
			"name": "p99_latency",
			"definition": map[string]string{
				"query": "histogram_quantile(0.99, rate(http_request_duration_seconds_bucket[5m]))",
				"unit":  "seconds",
			},
		})
	}, CreateKPIArgs{
		EntityID: "entity-abc",
		Name:     "p99_latency",
		Query:    "histogram_quantile(0.99, rate(http_request_duration_seconds_bucket[5m]))",
		Unit:     "seconds",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %s", text)
	}
	for _, want := range []string{"kpi-new", "p99_latency", "seconds", "Create once"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in:\n%s", want, text)
		}
	}
}
