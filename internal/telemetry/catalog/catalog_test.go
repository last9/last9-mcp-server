package catalog

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/constants"
	"last9-mcp/internal/models"
	logtelemetry "last9-mcp/internal/telemetry/logs"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestCatalogUsesExactBoundsAndOverfetchesTrustedInventory(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/datasources/ds-1/api-source-contracts/":
			_ = json.NewEncoder(w).Encode([]map[string]any{{
				"datasource": "prod", "source": "logs", "schema_version": 1,
				"fields":         map[string]string{"ServiceName": "string", "duration_ms": "milliseconds"},
				"backend_limits": map[string]any{"no_hidden_sampling": true, "max_rows": 101},
			}})
		case constants.EndpointLogsSeries:
			if r.URL.Query().Get("start") != "2025-10-09T08:53:20Z" || r.URL.Query().Get("end") != "2025-10-09T09:03:20Z" || r.URL.Query().Get("exact_bounds") != "true" {
				t.Fatalf("series bounds = %s", r.URL.RawQuery)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": []map[string]string{{"ServiceName": "checkout", "duration_ms": "12"}}, "l9_result": map[string]any{"partial": false}})
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
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": map[string]any{"result": []map[string]any{{"metric": map[string]any{"value": "checkout", "count": 9}, "values": []any{}}, {"metric": map[string]any{"value": "payments", "count": 4}, "values": []any{}}, {"metric": map[string]any{"value": "extra", "count": 1}, "values": []any{}}}}, "l9_result": map[string]any{"partial": false}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	result, _, err := NewHandler(server.Client(), testConfig(server.URL))(context.Background(), &mcp.CallToolRequest{}, CatalogArgs{
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
	result, _, err := NewHandler(server.Client(), testConfig(server.URL), testContracts(t, `[{"datasource":"prod","source":"traces","schema_version":1,"fields":{"ServiceName":"string"},"backend_limits":{"no_hidden_sampling":true,"max_rows":101}}]`))(context.Background(), &mcp.CallToolRequest{}, CatalogArgs{
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

func TestCatalogRejectsMalformedInventoryAsIncomplete(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == constants.EndpointLogsSeries {
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": []map[string]string{{"ServiceName": "checkout"}}, "l9_result": map[string]any{"partial": false}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":    "success",
			"data":      map[string]any{"result": []map[string]any{{"metric": map[string]any{"value": "checkout", "count": "not-a-count"}, "values": []any{}}}},
			"l9_result": map[string]any{"partial": false},
		})
	}))
	defer server.Close()
	contracts := testContracts(t, `[{"datasource":"prod","source":"logs","schema_version":1,"fields":{"ServiceName":"string"},"backend_limits":{"no_hidden_sampling":true,"max_rows":101}}]`)
	result, _, err := NewHandler(server.Client(), testConfig(server.URL), contracts)(context.Background(), &mcp.CallToolRequest{}, CatalogArgs{Datasource: "prod", Sources: []string{"logs"}, Protocol: "http", StartTimeISO: "2025-10-09T08:53:20Z", EndTimeISO: "2025-10-09T09:03:20Z", Include: []string{"services"}})
	if err != nil {
		t.Fatal(err)
	}
	var response CatalogResponse
	if err := json.Unmarshal([]byte(utils.GetTextContent(t, result)), &response); err != nil {
		t.Fatal(err)
	}
	if !response.Result.Partial || len(response.Services) != 0 {
		t.Fatalf("malformed inventory became authoritative: %#v", response)
	}
}

func TestCatalogRejectsNonSuccessSeriesAsIncomplete(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":    "error",
			"data":      []any{},
			"l9_result": map[string]any{"partial": false},
		})
	}))
	defer server.Close()
	contracts := testContracts(t, `[{"datasource":"prod","source":"logs","schema_version":1,"fields":{"ServiceName":"indexed"},"backend_limits":{"no_hidden_sampling":true,"max_rows":101}}]`)
	result, _, err := NewHandler(server.Client(), testConfig(server.URL), contracts)(context.Background(), &mcp.CallToolRequest{}, CatalogArgs{Datasource: "prod", Sources: []string{"logs"}, Protocol: "http", StartTimeISO: "2025-10-09T08:53:20Z", EndTimeISO: "2025-10-09T09:03:20Z", Include: []string{"fields"}})
	if err != nil {
		t.Fatal(err)
	}
	var response CatalogResponse
	if err := json.Unmarshal([]byte(utils.GetTextContent(t, result)), &response); err != nil {
		t.Fatal(err)
	}
	if !response.Result.Partial || len(response.Fields) != 0 {
		t.Fatalf("non-success series became authoritative: %#v", response)
	}
}

func TestCatalogRejectsSubsecondBounds(t *testing.T) {
	_, _, _, _, _, err := validateArgs(CatalogArgs{Datasource: "prod", Sources: []string{"logs"}, Protocol: "http", StartTimeISO: "2025-10-09T08:53:20.100Z", EndTimeISO: "2025-10-09T08:53:20.900Z", Include: []string{"fields"}}, testConfig("http://example.test"))
	if err == nil {
		t.Fatal("subsecond bounds must not be silently truncated")
	}
}

func TestCatalogEnvironmentFieldsAndBodyEvidenceStaySourceQualified(t *testing.T) {
	contract := SourceContract{Fields: map[string]string{"body.level": "body", "resource_deployment.environment": "indexed"}, MetricKinds: map[string]string{"resources['deployment.environment']": "environment", "resources['deployment.environment.name']": "environment"}}
	if got := environmentFields(contract); len(got) != 2 || got[0] != "resources['deployment.environment']" {
		t.Fatalf("environment fields = %#v", got)
	}
	evidence := logEvidence([]logtelemetry.LogAttribute{{Name: "body.level", FilterField: "attributes['level']", Source: "body", SampleCoverage: "2/5", SampleBodies: []string{"level=info"}, Hint: `[{"type":"parse","parser":"logfmt"}]`}}, contract, false, 5)
	if len(evidence) != 1 || evidence[0].Provenance != "body" || evidence[0].Parser != "logfmt" || evidence[0].Coverage != .4 || evidence[0].Complete {
		t.Fatalf("body evidence = %#v", evidence)
	}
}

func TestCatalogEnvironmentOnlyQueriesEveryConfiguredField(t *testing.T) {
	inventoryCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case constants.EndpointLogsSeries:
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": []map[string]string{{"ServiceName": "checkout"}}, "l9_result": map[string]any{"partial": false}})
		case constants.EndpointLogsQueryRange:
			inventoryCalls++
			data := map[string]any{"result": []map[string]any{{"metric": map[string]any{"value": r.URL.Query().Get("limit"), "count": 1}}}}
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": data, "l9_result": map[string]any{"partial": false}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	contracts := testContracts(t, "[{\"datasource\":\"prod\",\"source\":\"logs\",\"schema_version\":1,\"metric_kinds\":{\"resources['deployment.environment']\":\"environment\",\"resources['deployment.environment.name']\":\"environment\"},\"backend_limits\":{\"no_hidden_sampling\":true,\"max_rows\":101}}]")
	result, _, err := NewHandler(server.Client(), testConfig(server.URL), contracts)(context.Background(), &mcp.CallToolRequest{}, CatalogArgs{Datasource: "prod", Sources: []string{"logs"}, Protocol: "http", StartTimeISO: "2025-10-09T08:53:20Z", EndTimeISO: "2025-10-09T09:03:20Z", Include: []string{"environments"}})
	if err != nil {
		t.Fatal(err)
	}
	var response CatalogResponse
	if err := json.Unmarshal([]byte(utils.GetTextContent(t, result)), &response); err != nil {
		t.Fatal(err)
	}
	if response.Result.Partial || len(response.Environments) != 2 || inventoryCalls != 2 {
		t.Fatalf("environment-only request was not independently discovered: %#v calls=%d", response, inventoryCalls)
	}
}

func TestCatalogRejectsMalformedPartialAttestation(t *testing.T) {
	for _, attestation := range []any{map[string]any{}, map[string]any{"partial": nil}, map[string]any{"partial": "false"}} {
		t.Run("invalid", func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": []map[string]string{{"ServiceName": "checkout"}}, "l9_result": attestation})
			}))
			defer server.Close()
			contracts := testContracts(t, "[{\"datasource\":\"prod\",\"source\":\"logs\",\"schema_version\":1,\"fields\":{\"ServiceName\":\"indexed\"},\"backend_limits\":{\"no_hidden_sampling\":true,\"max_rows\":101}}]")
			result, _, err := NewHandler(server.Client(), testConfig(server.URL), contracts)(context.Background(), &mcp.CallToolRequest{}, CatalogArgs{Datasource: "prod", Sources: []string{"logs"}, Protocol: "http", StartTimeISO: "2025-10-09T08:53:20Z", EndTimeISO: "2025-10-09T09:03:20Z", Include: []string{"fields"}})
			if err != nil {
				t.Fatal(err)
			}
			var response CatalogResponse
			if err := json.Unmarshal([]byte(utils.GetTextContent(t, result)), &response); err != nil {
				t.Fatal(err)
			}
			if !response.Result.Partial {
				t.Fatalf("malformed attestation became complete: %#v", response)
			}
		})
	}
}

func TestCatalogRejectsMalformedInventoryPartialAttestation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case constants.EndpointLogsSeries:
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": []map[string]string{{"ServiceName": "checkout"}}, "l9_result": map[string]any{"partial": false}})
		case constants.EndpointLogsQueryRange:
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": map[string]any{"result": []map[string]any{}}, "l9_result": map[string]any{}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	contracts := testContracts(t, "[{\"datasource\":\"prod\",\"source\":\"logs\",\"schema_version\":1,\"fields\":{\"ServiceName\":\"indexed\"},\"backend_limits\":{\"no_hidden_sampling\":true,\"max_rows\":101}}]")
	result, _, err := NewHandler(server.Client(), testConfig(server.URL), contracts)(context.Background(), &mcp.CallToolRequest{}, CatalogArgs{Datasource: "prod", Sources: []string{"logs"}, Protocol: "http", StartTimeISO: "2025-10-09T08:53:20Z", EndTimeISO: "2025-10-09T09:03:20Z", Include: []string{"services"}})
	if err != nil {
		t.Fatal(err)
	}
	var response CatalogResponse
	if err := json.Unmarshal([]byte(utils.GetTextContent(t, result)), &response); err != nil {
		t.Fatal(err)
	}
	if !response.Result.Partial || len(response.Services) != 0 {
		t.Fatalf("malformed inventory attestation became authoritative: %#v", response)
	}
}

func TestCatalogMergesLogBodyEvidenceWithoutDroppingTraceFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case constants.EndpointTracesSeries:
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": []map[string]string{{"TraceOnly": "yes"}}, "l9_result": map[string]any{"partial": false}})
		case constants.EndpointLogsSeries:
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": []map[string]string{{"ServiceName": "checkout"}}, "l9_result": map[string]any{"partial": false}})
		case constants.EndpointLogsQueryRange:
			_ = json.NewEncoder(w).Encode(bodySampleResponse("{\"level\":\"info\"}", "{\"level\":\"warn\"}"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	contracts := testContracts(t, "[{\"datasource\":\"prod\",\"source\":\"traces\",\"schema_version\":1,\"fields\":{\"TraceOnly\":\"indexed\"},\"backend_limits\":{\"no_hidden_sampling\":true,\"max_rows\":101}},{\"datasource\":\"prod\",\"source\":\"logs\",\"schema_version\":1,\"fields\":{\"level\":\"body\"},\"backend_limits\":{\"no_hidden_sampling\":true,\"max_rows\":101}}]")
	result, _, err := NewHandler(server.Client(), testConfig(server.URL), contracts)(context.Background(), &mcp.CallToolRequest{}, CatalogArgs{Datasource: "prod", Sources: []string{"traces", "logs"}, Protocol: "http", StartTimeISO: "2025-10-09T08:53:20Z", EndTimeISO: "2025-10-09T09:03:20Z", Include: []string{"fields"}})
	if err != nil {
		t.Fatal(err)
	}
	var response CatalogResponse
	if err := json.Unmarshal([]byte(utils.GetTextContent(t, result)), &response); err != nil {
		t.Fatal(err)
	}
	if !hasField(response.Fields, "traces", "TraceOnly") || !hasField(response.Fields, "logs", "attributes['level']") {
		t.Fatalf("source evidence was overwritten: %#v", response.Fields)
	}
}

func TestCatalogMarksFailedBodySamplingIncomplete(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case constants.EndpointLogsSeries:
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": []map[string]string{{"ServiceName": "checkout"}}, "l9_result": map[string]any{"partial": false}})
		case constants.EndpointLogsQueryRange:
			http.Error(w, "body sample failed", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	contracts := testContracts(t, "[{\"datasource\":\"prod\",\"source\":\"logs\",\"schema_version\":1,\"fields\":{\"level\":\"body\"},\"backend_limits\":{\"no_hidden_sampling\":true,\"max_rows\":101}}]")
	result, _, err := NewHandler(server.Client(), testConfig(server.URL), contracts)(context.Background(), &mcp.CallToolRequest{}, CatalogArgs{Datasource: "prod", Sources: []string{"logs"}, Protocol: "http", StartTimeISO: "2025-10-09T08:53:20Z", EndTimeISO: "2025-10-09T09:03:20Z", Include: []string{"fields"}})
	if err != nil {
		t.Fatal(err)
	}
	var response CatalogResponse
	if err := json.Unmarshal([]byte(utils.GetTextContent(t, result)), &response); err != nil {
		t.Fatal(err)
	}
	if !response.Result.Partial || !hasIncompleteBodyField(response.Fields) {
		t.Fatalf("body sampling failure became complete: %#v", response)
	}
}

func TestCatalogMarksPartialBodySamplingIncomplete(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case constants.EndpointLogsSeries:
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": []map[string]string{{"ServiceName": "checkout"}}, "l9_result": map[string]any{"partial": false}})
		case constants.EndpointLogsQueryRange:
			sample := bodySampleResponse("{\"level\":\"info\"}")
			sample["l9_result"] = map[string]any{"partial": true, "reason": "sampled"}
			_ = json.NewEncoder(w).Encode(sample)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	contracts := testContracts(t, "[{\"datasource\":\"prod\",\"source\":\"logs\",\"schema_version\":1,\"fields\":{\"level\":\"body\"},\"backend_limits\":{\"no_hidden_sampling\":true,\"max_rows\":101}}]")
	result, _, err := NewHandler(server.Client(), testConfig(server.URL), contracts)(context.Background(), &mcp.CallToolRequest{}, CatalogArgs{Datasource: "prod", Sources: []string{"logs"}, Protocol: "http", StartTimeISO: "2025-10-09T08:53:20Z", EndTimeISO: "2025-10-09T09:03:20Z", Include: []string{"fields"}})
	if err != nil {
		t.Fatal(err)
	}
	var response CatalogResponse
	if err := json.Unmarshal([]byte(utils.GetTextContent(t, result)), &response); err != nil {
		t.Fatal(err)
	}
	if !response.Result.Partial {
		t.Fatalf("partial body sample became complete: %#v", response)
	}
}

func TestCatalogBodyEvidenceCarriesSampleCountAndExtraction(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case constants.EndpointLogsSeries:
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": []map[string]string{{"ServiceName": "checkout"}}, "l9_result": map[string]any{"partial": false}})
		case constants.EndpointLogsQueryRange:
			_ = json.NewEncoder(w).Encode(bodySampleResponse("{\"level\":\"info\"}", "{\"level\":\"warn\"}"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	contracts := testContracts(t, "[{\"datasource\":\"prod\",\"source\":\"logs\",\"schema_version\":1,\"fields\":{\"level\":\"body\"},\"backend_limits\":{\"no_hidden_sampling\":true,\"max_rows\":101}}]")
	result, _, err := NewHandler(server.Client(), testConfig(server.URL), contracts)(context.Background(), &mcp.CallToolRequest{}, CatalogArgs{Datasource: "prod", Sources: []string{"logs"}, Protocol: "http", StartTimeISO: "2025-10-09T08:53:20Z", EndTimeISO: "2025-10-09T09:03:20Z", Include: []string{"fields"}})
	if err != nil {
		t.Fatal(err)
	}
	var response CatalogResponse
	if err := json.Unmarshal([]byte(utils.GetTextContent(t, result)), &response); err != nil {
		t.Fatal(err)
	}
	for _, field := range response.Fields {
		if field.Source == "logs" && field.Provenance == "body" {
			if field.SampleCount != 2 || field.Coverage != 1 || field.Parser != "json" || field.Extraction["field"] != "Body" {
				t.Fatalf("incomplete body evidence: %#v", field)
			}
			labels, ok := field.Extraction["labels"].(map[string]any)
			if !ok || labels["level"] != "level" {
				t.Fatalf("extraction requirements were lost: %#v", field.Extraction)
			}
			return
		}
	}
	t.Fatalf("body field was not returned: %#v", response.Fields)
}

func bodySampleResponse(lines ...string) map[string]any {
	values := make([][]string, 0, len(lines))
	for i, line := range lines {
		values = append(values, []string{strconv.Itoa(i), line})
	}
	return map[string]any{"status": "success", "data": map[string]any{"resultType": "streams", "result": []map[string]any{{"values": values}}}}
}

func hasField(fields []FieldEvidence, source, field string) bool {
	for _, evidence := range fields {
		if evidence.Source == source && evidence.Field == field {
			return true
		}
	}
	return false
}

func hasIncompleteBodyField(fields []FieldEvidence) bool {
	for _, evidence := range fields {
		if evidence.Provenance == "body" && !evidence.Complete {
			return true
		}
	}
	return false
}

func testContracts(t *testing.T, body string) Contracts {
	t.Helper()
	contracts, err := DecodeContracts([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	return contracts
}

func testConfig(url string) models.Config {
	return models.Config{APIBaseURL: url, DatasourceName: "prod", Datasources: []models.DatasourceInfo{{ID: "ds-1", Name: "prod"}}, Region: "test", TokenManager: &auth.TokenManager{AccessToken: "token", ExpiresAt: time.Now().Add(time.Hour)}}
}
