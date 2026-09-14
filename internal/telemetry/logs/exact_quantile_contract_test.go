package logs_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/constants"
	"last9-mcp/internal/models"
	"last9-mcp/internal/telemetry/catalog"
	"last9-mcp/internal/telemetry/logs"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestGetLogsExactQuantileUsesManagedAPIContract(t *testing.T) {
	var contractRequests, queryRequests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/datasources/ds-1/api-source-contracts/":
			contractRequests++
			_, _ = w.Write([]byte(`[{"datasource":"prod","source":"logs","schema_version":1,"metric_kinds":{"attributes['duration_ms']":"exact_quantile"},"backend_limits":{"no_hidden_sampling":true,"max_rows":5000},"execution":{"record_unit":"log_record","parser_stages":[]}}]`))
		case constants.EndpointLogsQueryRange:
			queryRequests++
			_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"dataframe","result":[]}}`))
		default:
			http.Error(w, "unexpected path", http.StatusNotFound)
		}
	}))
	defer server.Close()

	cfg := models.Config{
		APIBaseURL: server.URL, DatasourceName: "prod", Region: "us-east-1", OrgSlug: "last9", ClusterID: "cluster-1",
		Datasources:  []models.DatasourceInfo{{ID: "ds-1", Name: "prod"}},
		TokenManager: &auth.TokenManager{AccessToken: "token", ExpiresAt: time.Now().Add(time.Hour)},
	}
	cfg.ExactQuantileAuthorizer = catalog.NewExactQuantileAuthorizer(server.Client(), cfg)
	handler := logs.NewGetLogsHandler(server.Client(), cfg)
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, logs.GetLogsArgs{
		LogjsonQuery: []map[string]interface{}{
			{"type": "filter", "query": map[string]interface{}{"$regex": []interface{}{"attributes['duration_ms']", "^[0-9]+(?:\\.[0-9]+)?$"}}},
			{"type": "aggregate", "aggregates": []interface{}{map[string]interface{}{"function": map[string]interface{}{"$quantile_exact": []interface{}{0.99, "attributes['duration_ms']"}}, "as": "p99"}}},
		},
		LookbackMinutes: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if contractRequests != 1 || queryRequests != 1 {
		t.Fatalf("contract requests = %d, query requests = %d", contractRequests, queryRequests)
	}
}
