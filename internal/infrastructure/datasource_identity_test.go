package infrastructure

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestSearchDatasourceIdentity(t *testing.T) {
	for _, requested := range []string{"", "source-a", "source-b"} {
		t.Run("requested_"+requested, func(t *testing.T) {
			requests := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requests++
				_ = json.NewEncoder(w).Encode([]instantPoint{{Metric: map[string]string{
					"cluster": "prod",
					"node":    "node-only-in-a",
				}}})
			}))
			defer srv.Close()

			cfg := testResolveConfig(srv.URL)
			cfg.ClusterID = "source-a"
			cfg.PrometheusReadURL = "https://source-a.invalid/read"
			result, _, err := NewSearchInfrastructureEntitiesHandler(srv.Client(), cfg)(context.Background(), &mcp.CallToolRequest{}, SearchInfrastructureEntitiesArgs{
				EntityType: "k8s_node",
				ClusterID:  requested,
				Timestamp:  1700000000,
			})
			if requested == "source-b" {
				if err == nil {
					t.Fatal("expected conflicting cluster_id to fail")
				}
				if requests != 0 {
					t.Fatalf("made %d upstream requests, want 0", requests)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			page := decodeSearchPage(t, result)
			if len(page.Entities) != 1 {
				t.Fatalf("got %d entities", len(page.Entities))
			}
			if got := page.Entities[0].ID; got != "k8s-node:source-a:prod:node-only-in-a" {
				t.Fatalf("id %q", got)
			}
		})
	}
}
