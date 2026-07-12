package apm

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
)

var deviationInputFields = []string{
	"service_name", "env", "datasource", "start_time_iso", "end_time_iso",
	"lookback_minutes", "baseline_start_time_iso", "baseline_end_time_iso",
	"max_services", "max_operations",
}

func validateDeviationInputSchema(t *testing.T, args any) error {
	t.Helper()
	schemaBytes, err := json.Marshal(GetAPMServiceDeviationsInputSchema())
	if err != nil {
		t.Fatalf("marshal schema: %v", err)
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal(schemaBytes, &schema); err != nil {
		t.Fatalf("unmarshal schema: %v", err)
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		t.Fatalf("resolve schema: %v", err)
	}
	return resolved.Validate(args)
}

func TestAPMServiceDeviationsInputSchemaStructure(t *testing.T) {
	schema := GetAPMServiceDeviationsInputSchema()
	if schema["type"] != "object" {
		t.Fatalf("type = %v, want object", schema["type"])
	}
	if schema["additionalProperties"] != false {
		t.Fatalf("additionalProperties = %v, want false", schema["additionalProperties"])
	}
	if _, exists := schema["required"]; exists {
		t.Fatal("top-level required must be omitted because every input is optional")
	}
	properties, ok := schema["properties"].(map[string]interface{})
	if !ok || len(properties) != len(deviationInputFields) {
		t.Fatalf("properties = %#v, want exactly %d fields", properties, len(deviationInputFields))
	}
	for _, name := range deviationInputFields {
		property, ok := properties[name].(map[string]interface{})
		if !ok {
			t.Fatalf("property %q missing", name)
		}
		if description, _ := property["description"].(string); strings.TrimSpace(description) == "" {
			t.Errorf("property %q has no useful description", name)
		}
	}
	descriptionRequirements := map[string][]string{
		"service_name":            {"Exact service", "Omit", "fleet"},
		"env":                     {"Exact environment", "never merged"},
		"datasource":              {"One datasource", "default"},
		"start_time_iso":          {"RFC3339", "end_time_iso", "lookback_minutes"},
		"end_time_iso":            {"RFC3339", "start_time_iso", "lookback_minutes"},
		"lookback_minutes":        {"default 60", "Mutually exclusive"},
		"baseline_start_time_iso": {"RFC3339", "baseline_end_time_iso", "equal duration"},
		"baseline_end_time_iso":   {"RFC3339", "baseline_start_time_iso", "equal duration"},
		"max_services":            {"Defaults to 10"},
		"max_operations":          {"Defaults to 10"},
	}
	for name, phrases := range descriptionRequirements {
		description := properties[name].(map[string]interface{})["description"].(string)
		for _, phrase := range phrases {
			if !strings.Contains(description, phrase) {
				t.Errorf("property %q description missing %q", name, phrase)
			}
		}
	}

	for _, name := range []string{"start_time_iso", "end_time_iso", "baseline_start_time_iso", "baseline_end_time_iso"} {
		property := properties[name].(map[string]interface{})
		if property["type"] != "string" || property["format"] != "date-time" {
			t.Errorf("timestamp %q = %#v, want string date-time", name, property)
		}
	}

	lookback := properties["lookback_minutes"].(map[string]interface{})
	if lookback["type"] != "number" || lookback["minimum"] != float64(1) || lookback["default"] != float64(60) {
		t.Errorf("lookback_minutes constraints = %#v", lookback)
	}
	for _, name := range []string{"max_services", "max_operations"} {
		property := properties[name].(map[string]interface{})
		want := map[string]interface{}{"type": "integer", "minimum": float64(1), "maximum": float64(10), "default": float64(10)}
		for key, value := range want {
			if !reflect.DeepEqual(property[key], value) {
				t.Errorf("%s.%s = %#v, want %#v", name, key, property[key], value)
			}
		}
	}

	dependent, ok := schema["dependentRequired"].(map[string]interface{})
	if !ok {
		t.Fatal("dependentRequired missing")
	}
	for field, pair := range map[string]string{
		"start_time_iso": "end_time_iso", "end_time_iso": "start_time_iso",
		"baseline_start_time_iso": "baseline_end_time_iso", "baseline_end_time_iso": "baseline_start_time_iso",
	} {
		if !reflect.DeepEqual(dependent[field], []string{pair}) {
			t.Errorf("dependentRequired[%q] = %#v, want [%q]", field, dependent[field], pair)
		}
	}
	wantAllOf := []interface{}{
		map[string]interface{}{"not": map[string]interface{}{"required": []string{"lookback_minutes", "start_time_iso"}}},
		map[string]interface{}{"not": map[string]interface{}{"required": []string{"lookback_minutes", "end_time_iso"}}},
	}
	if !reflect.DeepEqual(schema["allOf"], wantAllOf) {
		t.Fatalf("allOf = %#v, want %#v", schema["allOf"], wantAllOf)
	}
}

func TestAPMServiceDeviationsInputSchemaValidation(t *testing.T) {
	valid := []map[string]any{
		{},
		{"lookback_minutes": 60.5, "max_services": 10, "max_operations": 1},
		{"start_time_iso": "2026-07-12T08:00:00Z", "end_time_iso": "2026-07-12T09:00:00Z"},
		{"baseline_start_time_iso": "2026-07-11T06:00:00Z", "baseline_end_time_iso": "2026-07-11T08:00:00Z"},
	}
	for _, args := range valid {
		if err := validateDeviationInputSchema(t, args); err != nil {
			t.Errorf("valid args %#v rejected: %v", args, err)
		}
	}

	invalid := []map[string]any{
		{"unknown": true},
		{"start_time_iso": "2026-07-12T08:00:00Z"},
		{"end_time_iso": "2026-07-12T09:00:00Z"},
		{"baseline_start_time_iso": "2026-07-11T08:00:00Z"},
		{"baseline_end_time_iso": "2026-07-11T09:00:00Z"},
		{"lookback_minutes": 60, "start_time_iso": "2026-07-12T08:00:00Z", "end_time_iso": "2026-07-12T09:00:00Z"},
		{"lookback_minutes": 0},
		{"max_services": 11},
		{"max_operations": 0},
	}
	for _, args := range invalid {
		if err := validateDeviationInputSchema(t, args); err == nil {
			t.Errorf("invalid args %#v unexpectedly validated", args)
		}
	}
}
