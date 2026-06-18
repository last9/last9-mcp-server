package dashboards

import (
	"slices"
	"testing"
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
