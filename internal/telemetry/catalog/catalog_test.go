package catalog

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/constants"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestCatalogUsesExactBoundsAndOverfetchesTrustedInventory(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case constants.EndpointLogsSeries:
			if r.URL.Query().Get("start") != "1760000000" || r.URL.Query().Get("end") != "1760000600" {
				t.Fatalf("series bounds = %s", r.URL.RawQuery)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": []map[string]string{{"ServiceName": "checkout", "duration_ms": "12"}}})
		case constants.EndpointLogsQueryRange:
			if r.URL.Query().Get("limit") != "3" {
				t.Fatalf("catalog must overfetch: %s", r.URL.RawQuery)
			}
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			pipeline := body["pipeline"].([]any)
			if len(pipeline) != 1 || pipeline[0].(map[string]any)["type"] != "aggregate" {
				t.Fatalf("service inventory must have no operation filter: %#v", pipeline)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"result": []map[string]any{{"value": "checkout", "count": 9}, {"value": "payments", "count": 4}, {"value": "extra", "count": 1}}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	contracts := testContracts(t, `[{"datasource":"prod","source":"logs","schema_version":1,"fields":{"ServiceName":"string","duration_ms":"milliseconds"}}]`)
	result, _, err := NewHandler(server.Client(), testConfig(server.URL), contracts)(context.Background(), &mcp.CallToolRequest{}, CatalogArgs{
		Datasource: "prod", Sources: []string{"logs"}, Protocol: "http", StartTimeISO: "2025-10-09T08:53:20Z", EndTimeISO: "2025-10-09T09:03:20Z", Include: []string{"services", "fields"}, Limit: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	var response CatalogResponse
	if err := json.Unmarshal([]byte(utils.GetTextContent(t, result)), &response); err != nil {
		t.Fatal(err)
	}
	if !response.Result.Partial || len(response.Services) != 2 || response.Services[0].Value != "checkout" {
		t.Fatalf("unexpected bounded response: %#v", response)
	}
	if response.Services[0].Count != 9 {
		t.Fatalf("count = %d", response.Services[0].Count)
	}
}

func TestCatalogWithoutContractKeepsOnlyPositiveFieldObservations(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != constants.EndpointTracesSeries {
			t.Fatalf("unexpected request %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": []map[string]string{{"ServiceName": "checkout"}}})
	}))
	defer server.Close()
	result, _, err := NewHandler(server.Client(), testConfig(server.URL), Contracts{})(context.Background(), &mcp.CallToolRequest{}, CatalogArgs{
		Datasource: "prod", Sources: []string{"traces"}, Protocol: "http", StartTimeISO: "2025-10-09T08:53:20Z", EndTimeISO: "2025-10-09T09:03:20Z", Include: []string{"services", "fields"},
	})
	if err != nil {
		t.Fatal(err)
	}
	var response CatalogResponse
	if err := json.Unmarshal([]byte(utils.GetTextContent(t, result)), &response); err != nil {
		t.Fatal(err)
	}
	if !response.Result.Partial || len(response.Services) != 0 || len(response.Fields) != 1 || response.Fields[0].Complete {
		t.Fatalf("untrusted response = %#v", response)
	}
}

func TestCatalogPreservesBackendPartialMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": []map[string]string{{"ServiceName": "checkout"}}, "l9_result": map[string]any{"partial": true, "reason": "sampled"}})
	}))
	defer server.Close()
	result, _, err := NewHandler(server.Client(), testConfig(server.URL), testContracts(t, `[{"datasource":"prod","source":"traces","schema_version":1,"fields":{"ServiceName":"string"}}]`))(context.Background(), &mcp.CallToolRequest{}, CatalogArgs{
		Datasource: "prod", Sources: []string{"traces"}, Protocol: "http", StartTimeISO: "2025-10-09T08:53:20Z", EndTimeISO: "2025-10-09T09:03:20Z", Include: []string{"fields"},
	})
	if err != nil {
		t.Fatal(err)
	}
	var response CatalogResponse
	if err := json.Unmarshal([]byte(utils.GetTextContent(t, result)), &response); err != nil {
		t.Fatal(err)
	}
	if !response.Result.Partial || response.Result.Reason != "traces: sampled" || response.Fields[0].Complete {
		t.Fatalf("partial metadata was lost: %#v", response)
	}
}

func testContracts(t *testing.T, body string) Contracts {
	t.Helper()
	path := filepath.Join(t.TempDir(), "contracts.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	contracts, err := LoadContracts(path)
	if err != nil {
		t.Fatal(err)
	}
	return contracts
}

func testConfig(url string) models.Config {
	return models.Config{APIBaseURL: url, DatasourceName: "prod", Region: "test", TokenManager: &auth.TokenManager{AccessToken: "token", ExpiresAt: time.Now().Add(time.Hour)}}
}
