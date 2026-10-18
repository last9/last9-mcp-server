package dashboards

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
)

func assertDashboardObjectSchema(t *testing.T, schema map[string]interface{}, requiredFields []string) {
	t.Helper()

	props, ok := schema["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("InputSchema missing properties")
	}

	dashboard, ok := props["dashboard"].(map[string]interface{})
	if !ok {
		t.Fatalf("dashboard missing from properties")
	}
	if dashboard["type"] != "object" {
		t.Fatalf("dashboard type: want object, got %v", dashboard["type"])
	}

	metadata, ok := props["metadata"].(map[string]interface{})
	if !ok {
		t.Fatalf("metadata missing from properties")
	}
	if metadata["type"] != "object" {
		t.Fatalf("metadata type: want object, got %v", metadata["type"])
	}

	required, ok := schema["required"].([]string)
	if !ok {
		t.Fatalf("InputSchema missing required fields")
	}
	for _, field := range requiredFields {
		if !slices.Contains(required, field) {
			t.Fatalf("%q must be required, got %v", field, required)
		}
	}
}

func TestCreateDashboardInputSchema_DashboardIsObject(t *testing.T) {
	assertDashboardObjectSchema(t, GetCreateDashboardInputSchema(), []string{"dashboard"})
}

func TestUpdateDashboardInputSchema_DashboardIsObject(t *testing.T) {
	assertDashboardObjectSchema(t, GetUpdateDashboardInputSchema(), []string{"id", "dashboard"})
}

func validateInputSchema(t *testing.T, inputSchema map[string]interface{}, args any) error {
	t.Helper()

	schemaBytes, err := json.Marshal(inputSchema)
	if err != nil {
		t.Fatalf("json.Marshal(inputSchema) error = %v", err)
	}

	var schema jsonschema.Schema
	if err := json.Unmarshal(schemaBytes, &schema); err != nil {
		t.Fatalf("json.Unmarshal(inputSchema) error = %v", err)
	}

	resolved, err := schema.Resolve(nil)
	if err != nil {
		t.Fatalf("schema.Resolve() error = %v", err)
	}

	return resolved.Validate(args)
}

func TestCreateDashboardInputSchema_ValidatesDashboardObject(t *testing.T) {
	validArgs := map[string]any{
		"dashboard": map[string]any{
			"name": "Created",
			"panels": []any{map[string]any{
				"name": "Request rate", "version": 1,
				"layout":        map[string]any{"x": 0, "y": 0, "w": 12, "h": 8},
				"visualization": map[string]any{"type": "timeseries"},
				"queries": []any{map[string]any{
					"name": "A", "type": "range", "query_type": "promql", "telemetry": "metrics", "expr": "up",
					"legend": map[string]any{"type": "auto", "value": ""},
				}},
			}},
		},
		"metadata": map[string]any{
			"_category": "custom",
			"_type":     "metrics",
		},
	}

	if err := validateInputSchema(t, GetCreateDashboardInputSchema(), validArgs); err != nil {
		t.Fatalf("expected dashboard object args to validate: %v", err)
	}
}

// The panel version is pinned with bounds rather than an integer enum: some
// providers reject enums whose values are not strings.
func TestDashboardInputSchema_PinsPanelVersionToOne(t *testing.T) {
	for _, version := range []any{0, 1, 2} {
		args := map[string]any{
			"dashboard": map[string]any{
				"name": "Created",
				"panels": []any{map[string]any{
					"name": "Notes", "version": version,
					"layout":        map[string]any{"x": 0, "y": 0, "w": 12, "h": 8},
					"visualization": map[string]any{"type": "markdown", "markdown": "Synthetic notes"},
				}},
			},
		}
		err := validateInputSchema(t, GetCreateDashboardInputSchema(), args)
		if (err == nil) != (version == 1) {
			t.Errorf("version %v: validate error = %v", version, err)
		}
	}
}

func TestCreateDashboardInputSchema_RestrictsQueryTypes(t *testing.T) {
	args := map[string]any{
		"dashboard": map[string]any{
			"name": "Logs",
			"panels": []any{map[string]any{
				"name": "Errors", "version": 1,
				"layout":        map[string]any{"x": 0, "y": 0, "w": 12, "h": 8},
				"visualization": map[string]any{"type": "timeseries"},
				"queries": []any{map[string]any{
					"name": "A", "type": "range", "query_type": "log_ql", "telemetry": "logs", "expr": "{service_name=\"api\"}[5m]",
					"legend": map[string]any{"type": "auto", "value": ""},
				}},
			}},
		},
	}
	if err := validateInputSchema(t, GetCreateDashboardInputSchema(), args); err != nil {
		t.Fatalf("expected log_ql query to validate: %v", err)
	}

	query := args["dashboard"].(map[string]any)["panels"].([]any)[0].(map[string]any)["queries"].([]any)[0].(map[string]any)
	query["query_type"] = "logs"
	if err := validateInputSchema(t, GetCreateDashboardInputSchema(), args); err == nil {
		t.Fatal("expected telemetry name used as query_type to fail validation")
	}
	query["query_type"] = "log_ql"
	query["legend"].(map[string]any)["type"] = "invalid"
	if err := validateInputSchema(t, GetCreateDashboardInputSchema(), args); err == nil {
		t.Fatal("expected unsupported legend type to fail validation")
	}
}

func TestDashboardInputSchema_AllowsSupportedPanelVariants(t *testing.T) {
	queryPanel := map[string]any{
		"name": "Request rate", "version": 1,
		"layout":        map[string]any{"x": 0, "y": 0, "w": 12, "h": 8},
		"visualization": map[string]any{"type": "stat"},
		"queries": []any{map[string]any{
			"name": "A", "type": "range", "query_type": "promql", "telemetry": "metrics", "expr": "up",
			"legend": map[string]any{"type": "auto", "value": ""},
		}},
	}

	for _, tc := range []struct {
		name  string
		panel map[string]any
	}{
		{name: "query_control", panel: queryPanel},
		{name: "section", panel: map[string]any{
			"name":          "Overview",
			"layout":        map[string]any{"x": 0, "y": 0, "w": 24, "h": 1},
			"visualization": map[string]any{"type": "section"},
		}},
		{name: "markdown", panel: map[string]any{
			"name": "Notes", "version": 1,
			"layout":        map[string]any{"x": 0, "y": 1, "w": 24, "h": 3},
			"visualization": map[string]any{"type": "markdown", "markdown": "Synthetic notes"},
		}},
		{name: "default_version", panel: map[string]any{
			"name":          "Request rate",
			"layout":        map[string]any{"x": 0, "y": 0, "w": 12, "h": 8},
			"visualization": map[string]any{"type": "stat"},
			"queries": []any{map[string]any{
				"name": "A", "type": "range", "query_type": "promql", "telemetry": "metrics", "expr": "up",
				"legend": map[string]any{"type": "auto", "value": ""},
			}},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dashboard := map[string]any{"name": "Created", "panels": []any{tc.panel}}
			for _, op := range []string{"create", "update"} {
				args := map[string]any{"dashboard": dashboard}
				schema := GetCreateDashboardInputSchema()
				if op == "update" {
					args["id"] = "uuid-1"
					schema = GetUpdateDashboardInputSchema()
				}
				if err := validateInputSchema(t, schema, args); err != nil {
					t.Fatalf("%s schema rejected supported %s panel: %v", op, tc.name, err)
				}
			}
		})
	}
}

func TestDashboardInputSchema_RequiresQueriesForQueryPanels(t *testing.T) {
	args := map[string]any{"dashboard": map[string]any{
		"name": "Created",
		"panels": []any{map[string]any{
			"name":          "Request rate",
			"layout":        map[string]any{"x": 0, "y": 0, "w": 12, "h": 8},
			"visualization": map[string]any{"type": "stat"},
		}},
	}}

	if err := validateInputSchema(t, GetCreateDashboardInputSchema(), args); err == nil {
		t.Fatal("expected query panel without queries to fail validation")
	}
}

func TestDashboardInputSchema_RequiresLogsConfigForLogsVisualization(t *testing.T) {
	args := map[string]any{"dashboard": map[string]any{
		"name": "Created",
		"panels": []any{map[string]any{
			"name": "Recent errors", "version": 1,
			"layout":        map[string]any{"x": 0, "y": 0, "w": 12, "h": 8},
			"visualization": map[string]any{"type": "logs"},
			"queries": []any{map[string]any{
				"name": "Errors", "type": "range", "query_type": "log_ql", "telemetry": "logs", "expr": "{}",
				"legend": map[string]any{"type": "auto", "value": ""},
			}},
		}},
	}}
	if err := validateInputSchema(t, GetCreateDashboardInputSchema(), args); err == nil {
		t.Fatal("expected logs visualization without logs_config to fail validation")
	}
	panel := args["dashboard"].(map[string]any)["panels"].([]any)[0].(map[string]any)
	panel["visualization"].(map[string]any)["logs_config"] = map[string]any{"columns": []any{"timestamp", "severity", "body"}}
	if err := validateInputSchema(t, GetCreateDashboardInputSchema(), args); err != nil {
		t.Fatalf("expected supported logs_config to validate: %v", err)
	}
	panel["visualization"].(map[string]any)["logs_config"].(map[string]any)["columns"] = []any{"unknown"}
	if err := validateInputSchema(t, GetCreateDashboardInputSchema(), args); err == nil {
		t.Fatal("expected unsupported logs_config column to fail validation")
	}
}

func TestCreateDashboardInputSchema_RejectsLegacyPanelShape(t *testing.T) {
	args := map[string]any{"dashboard": map[string]any{
		"name": "Created",
		"panels": []any{map[string]any{
			"name":    "Log Volume",
			"type":    "timeseries",
			"gridPos": map[string]any{"x": 0, "y": 0, "w": 12, "h": 8},
			"queries": []any{map[string]any{"query": "sum(rate(logs_total[5m]))"}},
		}},
	}}

	if err := validateInputSchema(t, GetCreateDashboardInputSchema(), args); err == nil {
		t.Fatal("expected legacy panel shape to fail validation")
	}
}

func TestCreateDashboardInputSchema_RejectsMissingDashboard(t *testing.T) {
	args := map[string]any{
		"metadata": map[string]any{
			"_category": "custom",
			"_type":     "metrics",
		},
	}

	if err := validateInputSchema(t, GetCreateDashboardInputSchema(), args); err == nil {
		t.Fatal("expected missing dashboard to fail validation")
	}
}

func TestCreateDashboardInputSchema_RejectsRawMessageByteArray(t *testing.T) {
	args := map[string]any{
		"dashboard": []any{123, 34, 110, 97, 109, 101, 34, 58, 34, 120, 34, 125},
	}

	if err := validateInputSchema(t, GetCreateDashboardInputSchema(), args); err == nil {
		t.Fatal("expected byte-array dashboard payload to fail validation")
	}
}

func TestCreateDashboardInputSchema_AcceptsNameOnlyDashboard(t *testing.T) {
	nameOnly := map[string]any{"dashboard": map[string]any{"name": "Checkout", "panels": []any{}}}
	if err := validateInputSchema(t, GetCreateDashboardInputSchema(), nameOnly); err != nil {
		t.Fatalf("expected name-only dashboard with empty panels to validate: %v", err)
	}

	missingPanels := map[string]any{"dashboard": map[string]any{"name": "Checkout"}}
	if err := validateInputSchema(t, GetCreateDashboardInputSchema(), missingPanels); err == nil {
		t.Fatal("expected dashboard without panels to fail validation")
	}
}

func TestUpdateDashboardInputSchema_RequiresIDAndDashboard(t *testing.T) {
	validArgs := map[string]any{
		"id": "uuid-1",
		"dashboard": map[string]any{
			"name":   "Updated",
			"panels": []any{},
		},
	}
	if err := validateInputSchema(t, GetUpdateDashboardInputSchema(), validArgs); err != nil {
		t.Fatalf("expected update args to validate: %v", err)
	}

	missingIDArgs := map[string]any{
		"dashboard": map[string]any{
			"name":   "Updated",
			"panels": []any{},
		},
	}
	if err := validateInputSchema(t, GetUpdateDashboardInputSchema(), missingIDArgs); err == nil {
		t.Fatal("expected update args without id to fail validation")
	}
}

func TestDashboardInputSchema_ValidatesRawLogsAndCSV(t *testing.T) {
	query := func(queryType, telemetry, expr string) []any {
		return []any{map[string]any{
			"name": "A", "type": "range", "query_type": queryType, "telemetry": telemetry, "expr": expr,
			"legend": map[string]any{"type": "auto", "value": ""},
		}}
	}
	csv := map[string]any{"columns": map[string]any{}}
	for _, tc := range []struct {
		name          string
		visualization string
		fields        map[string]any
		wantErr       bool
	}{
		{name: "metrics_control", visualization: "stat", fields: map[string]any{
			"queries": query("promql", "metrics", "up"),
		}},
		{name: "raw_logs", visualization: "logs", fields: map[string]any{
			"visualization": map[string]any{
				"type":        "logs",
				"logs_config": map[string]any{"columns": []any{"timestamp", "severity", "body"}},
			},
			"queries": query("log_raw", "logs", `[{"type":"filter","query":{"$eq":["ServiceName","synthetic-api"]}}]`),
		}},
		{name: "csv_table", visualization: "table", fields: map[string]any{"csv": csv}},
		{name: "csv_chart", visualization: "bar", fields: map[string]any{"csv": csv}},
		{name: "missing_queries", visualization: "stat", wantErr: true},
		{name: "null_csv", visualization: "table", fields: map[string]any{"csv": nil}, wantErr: true},
		{name: "string_csv", visualization: "table", fields: map[string]any{"csv": "data.csv"}, wantErr: true},
		{name: "array_csv", visualization: "table", fields: map[string]any{"csv": []any{}}, wantErr: true},
		{name: "csv_with_invalid_query", visualization: "table", wantErr: true, fields: map[string]any{
			"csv": csv, "queries": []any{map[string]any{"expr": "up"}},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			panel := map[string]any{
				"name": "Synthetic panel", "version": 1,
				"layout":        map[string]any{"x": 0, "y": 0, "w": 12, "h": 8},
				"visualization": map[string]any{"type": tc.visualization},
			}
			for field, value := range tc.fields {
				panel[field] = value
			}
			dashboard := map[string]any{
				"name": "Synthetic dashboard", "panels": []any{panel},
				"csv_datasource": map[string]any{
					"raw_csv": "label,value\nalpha,1\n",
					"columns": []any{
						map[string]any{"name": "label", "type": "string"},
						map[string]any{"name": "value", "type": "number"},
					},
				},
			}
			for _, op := range []string{"create", "update"} {
				t.Run(op, func(t *testing.T) {
					args := map[string]any{"dashboard": dashboard}
					schema := GetCreateDashboardInputSchema()
					if op == "update" {
						args["id"] = "synthetic-id"
						schema = GetUpdateDashboardInputSchema()
					}
					err := validateInputSchema(t, schema, args)
					if (err != nil) != tc.wantErr {
						t.Fatalf("validation error = %v, want error = %v", err, tc.wantErr)
					}
				})
			}
		})
	}
}
