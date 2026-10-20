package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"last9-mcp/internal/constants"
	"last9-mcp/internal/utils"
)

func TestReviewCatalogPreservesExactInventoryCounts(t *testing.T) {
	for _, count := range []int64{9007199254740993} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == constants.EndpointLogsSeries {
					fmt.Fprint(w, `{"status":"success","data":[{"service":""}],"l9_result":{"partial":false}}`)
					return
				}
				fmt.Fprintf(w, `{"status":"success","data":{"result":[{"metric":{"value":"checkout","count":%d}}]},"l9_result":{"partial":false}}`, count)
			}))
			defer server.Close()
			contracts := testContracts(t, `[{"datasource":"prod","source":"logs","schema_version":1,"backend_limits":{"no_hidden_sampling":true,"max_rows":101}}]`)
			result, _, err := NewHandler(server.Client(), testConfig(server.URL), contracts)(context.Background(), nil, CatalogArgs{Datasource: "prod", Sources: []string{"logs"}, Protocol: "http", StartTimeISO: "2025-10-09T08:53:20Z", EndTimeISO: "2025-10-09T09:03:20Z", Include: []string{"services"}})
			if err != nil {
				t.Fatal(err)
			}
			var response CatalogResponse
			if err := json.Unmarshal([]byte(utils.GetTextContent(t, result)), &response); err != nil {
				t.Fatal(err)
			}
			if response.Result.Partial || len(response.Services) != 1 || response.Services[0].Count != count {
				t.Fatalf("want complete count %d; got services=%+v result=%+v", count, response.Services, response.Result)
			}
		})
	}
}

func TestReviewCatalogMissingBodyAttestationIsPartial(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == constants.EndpointLogsSeries {
			fmt.Fprint(w, `{"status":"success","data":[{"service":""}],"l9_result":{"partial":false}}`)
			return
		}
		// Existing fixture intentionally has no l9_result envelope.
		json.NewEncoder(w).Encode(bodySampleResponse(`{"level":"info"}`))
	}))
	defer server.Close()
	contracts := testContracts(t, `[{"datasource":"prod","source":"logs","schema_version":1,"fields":{"level":"body"},"backend_limits":{"no_hidden_sampling":true,"max_rows":101}}]`)
	result, _, err := NewHandler(server.Client(), testConfig(server.URL), contracts)(context.Background(), nil, CatalogArgs{Datasource: "prod", Sources: []string{"logs"}, Protocol: "http", StartTimeISO: "2025-10-09T08:53:20Z", EndTimeISO: "2025-10-09T09:03:20Z", Include: []string{"fields"}})
	if err != nil {
		t.Fatal(err)
	}
	var response CatalogResponse
	if err := json.Unmarshal([]byte(utils.GetTextContent(t, result)), &response); err != nil {
		t.Fatal(err)
	}
	if !response.Result.Partial {
		t.Fatalf("unattested body sample reported complete: %+v", response.Result)
	}
}
