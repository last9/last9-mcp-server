package catalog

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"last9-mcp/internal/constants"
	"last9-mcp/internal/utils"
)

func TestCatalogTransportsOnlySelectedOperatorDescriptor(t *testing.T) {
	contracts := testContracts(t, `[{"datasource":"prod","source":"logs","schema_version":1,"backend_limits":{"no_hidden_sampling":true,"max_rows":5000},"execution":{"record_unit":"log_record","status_field":"attributes['status']","status_coverage":"complete","parser_stages":[],"dimension_fields":{"user_agent":"attributes['user_agent']"}}}]`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": []any{}, "l9_result": map[string]any{"partial": false}})
	}))
	defer server.Close()
	result, _, err := NewHandler(server.Client(), testConfig(server.URL), contracts)(context.Background(), nil, CatalogArgs{Datasource: "prod", Sources: []string{"logs"}, Protocol: "http", StartTimeISO: "2025-10-09T08:53:20.000Z", EndTimeISO: "2025-10-09T09:03:20.000Z", Include: []string{"fields"}})
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(utils.GetTextContent(t, result)), &body); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"requested", "effective"} {
		scope := body[name].(map[string]any)
		if scope["start_time_iso"] != "2025-10-09T08:53:20.000Z" || scope["end_time_iso"] != "2025-10-09T09:03:20.000Z" {
			t.Fatalf("%s must preserve accepted frozen timestamp bytes: %#v", name, scope)
		}
	}
	descriptors, ok := body["descriptors"].([]any)
	if !ok || len(descriptors) != 1 {
		t.Fatalf("missing descriptor: %#v", body)
	}
	if descriptors[0].(map[string]any)["execution"].(map[string]any)["record_unit"] != "log_record" {
		t.Fatal("descriptor changed")
	}
}

func TestCatalogDiscoversExecutionEnvironmentWithoutLegacyDeclaration(t *testing.T) {
	for _, tc := range []struct {
		name, legacy string
		fields       []string
	}{
		{"execution only", `{}`, []string{"resources['environment']"}},
		{"duplicate", `{"resources['environment']":"environment"}`, []string{"resources['environment']"}},
		{"combined", `{"resources['environment']":"environment","resources['deployment.environment']":"environment"}`, []string{"resources['deployment.environment']", "resources['environment']"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			contracts := testContracts(t, `[{"datasource":"prod","source":"logs","schema_version":1,"metric_kinds":`+tc.legacy+`,"backend_limits":{"no_hidden_sampling":true,"max_rows":5000},"execution":{"record_unit":"log_record","parser_stages":[],"environment_field":"resources['environment']"}}]`)
			var queried []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == constants.EndpointLogsSeries {
					_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": []any{}, "l9_result": map[string]any{"partial": false}})
					return
				}
				if r.URL.Path != constants.EndpointLogsQueryRange {
					t.Errorf("unexpected endpoint: %s", r.URL.Path)
					http.NotFound(w, r)
					return
				}
				var body struct {
					Pipeline []struct {
						Groupby map[string]string `json:"groupby"`
					} `json:"pipeline"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
					return
				}
				if len(body.Pipeline) != 1 || len(body.Pipeline[0].Groupby) != 1 {
					t.Errorf("invalid inventory query: %#v", body)
					return
				}
				for field, alias := range body.Pipeline[0].Groupby {
					if alias != "value" {
						t.Errorf("unexpected alias: %s", alias)
					}
					queried = append(queried, field)
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": map[string]any{"result": []any{map[string]any{"metric": map[string]any{"value": "prod", "count": 3}, "values": []any{}}}}, "l9_result": map[string]any{"partial": false}})
			}))
			defer server.Close()
			result, _, err := NewHandler(server.Client(), testConfig(server.URL), contracts)(context.Background(), nil, CatalogArgs{Datasource: "prod", Sources: []string{"logs"}, Protocol: "http", StartTimeISO: "2025-10-09T08:53:20Z", EndTimeISO: "2025-10-09T09:03:20Z", Include: []string{"environments"}})
			if err != nil {
				t.Fatal(err)
			}
			var response CatalogResponse
			if err := json.Unmarshal([]byte(utils.GetTextContent(t, result)), &response); err != nil {
				t.Fatal(err)
			}
			if response.Result.Partial || response.Result.Reason != "" {
				t.Fatalf("configured environment returned incomplete: %#v", response.Result)
			}
			if !reflect.DeepEqual(queried, tc.fields) {
				t.Fatalf("queried fields = %v, want %v", queried, tc.fields)
			}
			if len(response.Environments) != len(tc.fields) || len(response.Descriptors) != 1 {
				t.Fatalf("missing environment/descriptor: %#v", response)
			}
			for i, row := range response.Environments {
				if row.Source != "logs" || row.Field != tc.fields[i] || row.Value != "prod" || row.Count != 3 {
					t.Fatalf("unexpected environment: %#v", row)
				}
			}
		})
	}
}

func TestCatalogParsesBodyEnvironmentBeforeInventory(t *testing.T) {
	contracts := testContracts(t, `[{"datasource":"prod","source":"logs","schema_version":1,"backend_limits":{"no_hidden_sampling":true,"max_rows":5000},"execution":{"record_unit":"log_record","parser_stages":[{"type":"parse","parser":"json","field":"Body","labels":{"environment":"env"}}],"environment_field":"attributes['environment']"}}]`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == constants.EndpointLogsSeries {
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": []any{}, "l9_result": map[string]any{"partial": false}})
			return
		}
		if r.URL.Path != constants.EndpointLogsQueryRange {
			http.NotFound(w, r)
			return
		}
		var body struct {
			Pipeline []map[string]any `json:"pipeline"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		parsed := len(body.Pipeline) == 2 && body.Pipeline[0]["type"] == "parse" && body.Pipeline[0]["parser"] == "json" && body.Pipeline[0]["field"] == "Body" && body.Pipeline[1]["type"] == "aggregate"
		if parsed {
			if labels, ok := body.Pipeline[0]["labels"].(map[string]any); !ok || labels["environment"] != "env" {
				parsed = false
			}
			if groupby, ok := body.Pipeline[1]["groupby"].(map[string]any); !ok || groupby["attributes['environment']"] != "value" {
				parsed = false
			}
		}
		if !parsed {
			t.Errorf("inventory must parse Body before grouping environment: %#v", body.Pipeline)
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": map[string]any{"result": []any{}}, "l9_result": map[string]any{"partial": false}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": map[string]any{"result": []any{map[string]any{"metric": map[string]any{"value": "prod", "count": 1}}}}, "l9_result": map[string]any{"partial": false}})
	}))
	defer server.Close()

	result, _, err := NewHandler(server.Client(), testConfig(server.URL), contracts)(context.Background(), nil, CatalogArgs{Datasource: "prod", Sources: []string{"logs"}, Protocol: "http", StartTimeISO: "2025-10-09T08:53:20Z", EndTimeISO: "2025-10-09T09:03:20Z", Include: []string{"environments"}})
	if err != nil {
		t.Fatal(err)
	}
	var response CatalogResponse
	if err := json.Unmarshal([]byte(utils.GetTextContent(t, result)), &response); err != nil {
		t.Fatal(err)
	}
	if response.Result.Partial || len(response.Environments) != 1 || response.Environments[0].Value != "prod" {
		t.Fatalf("unparsed environment receipt became authoritative: %#v", response)
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
		body := `[{"datasource":"prod","source":"logs","schema_version":1,"backend_limits":{"no_hidden_sampling":true,"max_rows":5000},"execution":` + execution + `}]`
		if _, err := DecodeContracts([]byte(body)); err == nil {
			t.Fatalf("accepted unsafe execution %s", execution)
		}
	}
}
