package infrastructure

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestInfrastructureToolErrorsNeverRelayCredentialsOrURLs(t *testing.T) {
	leaks := []string{"internal.invalid", "synthetic-secret", "synthetic-bearer"}
	for _, status := range []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusInternalServerError} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				fmt.Fprint(w, `{"error":"upstream https://internal.invalid/debug password=synthetic-secret Bearer synthetic-bearer"}`)
			}))
			defer srv.Close()
			cfg := testResolveConfig(srv.URL)
			cfg.PrometheusReadURL = "https://prometheus.invalid/read"

			_, _, resolveErr := NewGetInfrastructureContextHandler(srv.Client(), cfg)(context.Background(), &mcp.CallToolRequest{}, GetInfrastructureContextArgs{
				EntityType: "host",
				Selectors:  InfrastructureSelectors{HostName: "synthetic-host"},
				Timestamp:  1700000000,
			})
			_, _, searchErr := NewSearchInfrastructureEntitiesHandler(srv.Client(), cfg)(context.Background(), &mcp.CallToolRequest{}, SearchInfrastructureEntitiesArgs{
				EntityType: "host",
				Timestamp:  1700000000,
			})

			for name, err := range map[string]error{"resolve": resolveErr, "search": searchErr} {
				if err == nil {
					t.Fatalf("%s: want error for HTTP %d", name, status)
				}
				if !strings.Contains(err.Error(), fmt.Sprint(status)) {
					t.Errorf("%s: error should name HTTP %d: %v", name, status, err)
				}
				for _, leak := range leaks {
					if strings.Contains(err.Error(), leak) {
						t.Errorf("%s: error exposes %q: %v", name, leak, err)
					}
				}
			}
		})
	}
}
