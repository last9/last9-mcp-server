package infrastructure

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestSearchPodLinkIdentity(t *testing.T) {
	for _, uid := range []string{"", "uid-original"} {
		t.Run("uid_"+uid, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_ = json.NewEncoder(w).Encode([]instantPoint{{Metric: map[string]string{
					"cluster":   "prod",
					"namespace": "default",
					"pod":       "api-0",
					"uid":       uid,
				}}})
			}))
			defer srv.Close()

			cfg := testResolveConfig(srv.URL)
			cfg.PrometheusReadURL = "https://prometheus.invalid/read"
			result, _, err := NewSearchInfrastructureEntitiesHandler(srv.Client(), cfg)(context.Background(), &mcp.CallToolRequest{}, SearchInfrastructureEntitiesArgs{
				EntityType: "k8s_pod",
				Timestamp:  1700000000,
			})
			if err != nil {
				t.Fatal(err)
			}
			page := decodeSearchPage(t, result)
			if len(page.Entities) != 1 {
				t.Fatalf("got %d entities", len(page.Entities))
			}
			href, err := url.Parse(page.Entities[0].UI.Href)
			if err != nil {
				t.Fatal(err)
			}
			if got := href.Query().Get("uid"); got != uid {
				t.Errorf("uid query parameter %q, want %q", got, uid)
			}
		})
	}
}
