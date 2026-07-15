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
			"name":   "Created",
			"panels": []any{},
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
