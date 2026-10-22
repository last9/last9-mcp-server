package infrastructure

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestReviewInventoryCap(t *testing.T) {
	for _, count := range []int{500, 501} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				points := make([]instantPoint, count)
				for i := range points {
					j := i
					if calls == 2 {
						j = count - 1 - i
					}
					points[i] = instantPoint{Metric: map[string]string{"nodename": fmt.Sprintf("host-%03d", j)}}
				}
				_ = json.NewEncoder(w).Encode(points)
			}))
			defer srv.Close()
			cfg := testResolveConfig(srv.URL)
			cfg.PrometheusReadURL = "https://prom.example/read"
			handler := NewSearchInfrastructureEntitiesHandler(srv.Client(), cfg)
			args := SearchInfrastructureEntitiesArgs{EntityType: "host", Timestamp: 1700000000, Limit: 100}
			first, _, err := handler(context.Background(), &mcp.CallToolRequest{}, args)
			if err != nil {
				t.Fatal(err)
			}
			args.Cursor = decodeSearchPage(t, first).NextCursor
			second, _, err := handler(context.Background(), &mcp.CallToolRequest{}, args)
			if err != nil {
				t.Fatal(err)
			}
			page := decodeSearchPage(t, second)
			if len(page.Entities) == 0 || page.Entities[0].Attributes["host_name"] != "host-100" {
				t.Errorf("same inventory/time, reversed response order: second page starts at %v; want host-100", page.Entities[0].Attributes)
			}
			args.Cursor = ""
			args.Query = fmt.Sprintf("host-%03d", count-1)
			result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, args)
			if err != nil {
				t.Fatal(err)
			}
			page = decodeSearchPage(t, result)
			if len(page.Entities) != 1 || page.Entities[0].Attributes["host_name"] != args.Query {
				t.Errorf("exact-name search %q: got %d matches, truncated=%v; want one", args.Query, len(page.Entities), page.Truncated)
			}
		})
	}
}
