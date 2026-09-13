package catalog

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"last9-mcp/internal/utils"
)

func TestCatalogTransportsOnlySelectedOperatorDescriptor(t *testing.T) {
	contracts := testContracts(t, `[{"datasource":"prod","source":"logs","schema_version":1,"backend_limits":{"no_hidden_sampling":true,"max_rows":5000},"execution":{"record_unit":"log_record","status_field":"attributes['status']","status_coverage":"complete","parser_stages":[],"dimension_fields":{"user_agent":"attributes['user_agent']"}}}]`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": []any{}, "l9_result": map[string]any{"partial": false}})
	}))
	defer server.Close()
	result, _, err := NewHandler(server.Client(), testConfig(server.URL), contracts)(context.Background(), nil, CatalogArgs{Datasource: "prod", Sources: []string{"logs"}, Protocol: "http", StartTimeISO: "2025-10-09T08:53:20Z", EndTimeISO: "2025-10-09T09:03:20Z", Include: []string{"fields"}})
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(utils.GetTextContent(t, result)), &body); err != nil {
		t.Fatal(err)
	}
	descriptors, ok := body["descriptors"].([]any)
	if !ok || len(descriptors) != 1 {
		t.Fatalf("missing descriptor: %#v", body)
	}
	if descriptors[0].(map[string]any)["execution"].(map[string]any)["record_unit"] != "log_record" {
		t.Fatal("descriptor changed")
	}
}

func TestExecutableDescriptorStartupRefusals(t *testing.T) {
	for _, execution := range []string{
		`{"record_unit":"log_record"}`,
		`{"record_unit":"log_record","parser_stages":[],"status_field":"Status"}`,
		`{"record_unit":"log_record","parser_stages":[],"status_field":"Status","status_coverage":"sampled"}`,
		`{"record_unit":"log_record","parser_stages":[],"dimension_fields":{"bad key":"Body"}}`,
		`{"record_unit":"log_record","parser_stages":[],"dimension_fields":{"tenant":"attributes['bad'] OR 1"}}`,
		`{"record_unit":"log_record","parser_stages":[{"type":"filter","parser":"json"}]}`,
		`{"record_unit":"log_record","parser_stages":[{"type":"parse","parser":"json","query":"x"}]}`,
		`{"record_unit":"log_record","parser_stages":[{"type":"parse","parser":"regexp","pattern":"["}]}`,
		`{"record_unit":"log_record","parser_stages":[],"metric_kind":"counter","metric_unit":"requests"}`,
		`{"record_unit":"log_record","parser_stages":[],"graphql":{"qualified":false}}`,
		`{"record_unit":"log_record","record_unit":"request","parser_stages":[]}`,
	} {
		path := filepath.Join(t.TempDir(), "contracts.json")
		body := `[{"datasource":"prod","source":"logs","schema_version":1,"backend_limits":{"no_hidden_sampling":true,"max_rows":5000},"execution":` + execution + `}]`
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadContracts(path); err == nil {
			t.Fatalf("accepted unsafe execution %s", execution)
		}
	}
}
