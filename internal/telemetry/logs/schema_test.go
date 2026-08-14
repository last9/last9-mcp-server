package logs

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
)

func schemaPropertiesForType[T any](t *testing.T) map[string]interface{} {
	t.Helper()

	schema, err := jsonschema.ForType(reflect.TypeFor[T](), &jsonschema.ForOptions{})
	if err != nil {
		t.Fatalf("failed to generate schema: %v", err)
	}

	raw, err := json.Marshal(schema)
	if err != nil {
		t.Fatalf("failed to marshal schema: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("failed to unmarshal schema JSON: %v", err)
	}

	props, ok := parsed["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("schema has no properties, got: %s", string(raw))
	}

	return props
}

// hasArrayType returns true if the JSON Schema "type" value includes "array".
// The type can be a string ("array") or a slice (["null", "array"]).
func hasArrayType(typ interface{}) bool {
	switch v := typ.(type) {
	case string:
		return v == "array"
	case []interface{}:
		for _, t := range v {
			if t == "array" {
				return true
			}
		}
	}
	return false
}

// TestGetLogsArgs_SchemaCompatibility ensures the generated JSON Schema for
// GetLogsArgs does not contain "items": true for the logjson_query field.
//
// OpenAI and other strict JSON Schema validators reject boolean values for
// "items" (valid only in draft-07+). The field must use []map[string]interface{}
// so the schema emits "items": {"type": "object"} instead.
func TestGetLogsArgs_SchemaCompatibility(t *testing.T) {
	props := schemaPropertiesForType[GetLogsArgs](t)

	field, ok := props["logjson_query"].(map[string]interface{})
	if !ok {
		t.Fatalf("logjson_query not found in schema properties")
	}

	// Must include "array" in its type (may also include "null" due to omitempty).
	if !hasArrayType(field["type"]) {
		t.Errorf("logjson_query: expected type to include array, got %v", field["type"])
	}

	// "items" must not be a boolean true — that is rejected by OpenAI and
	// other strict providers.
	items := field["items"]
	if items == nil {
		t.Fatal("logjson_query: items is missing from schema")
	}
	if _, isBool := items.(bool); isBool {
		t.Errorf("logjson_query: items must be an object schema, not a boolean (got %v). "+
			"This breaks OpenAI and other strict JSON Schema validators. "+
			"Fix: use []map[string]interface{} instead of []interface{}", items)
	}

	// "items" must be an object schema with type=object.
	itemsMap, ok := items.(map[string]interface{})
	if !ok {
		t.Fatalf("logjson_query: items is not an object, got %T: %v", items, items)
	}
	if itemsMap["type"] != "object" {
		t.Errorf("logjson_query: items.type expected=object, got %v", itemsMap["type"])
	}
}

func TestLogToolsSchemaIncludesIndex(t *testing.T) {
	tests := []struct {
		name  string
		props map[string]interface{}
	}{
		{
			name:  "get_logs",
			props: schemaPropertiesForType[GetLogsArgs](t),
		},
		{
			name:  "get_log_attributes",
			props: schemaPropertiesForType[GetLogAttributesArgs](t),
		},
		{
			name:  "get_service_logs",
			props: schemaPropertiesForType[GetServiceLogsArgs](t),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			field, ok := tt.props["index"].(map[string]interface{})
			if !ok {
				t.Fatalf("index not found in schema properties")
			}

			typeValue, ok := field["type"].(string)
			if !ok || typeValue != "string" {
				t.Fatalf("index.type expected string, got %v", field["type"])
			}
		})
	}
}

func TestGetLogsArgs_LookbackDescriptionMatchesPromptDefault(t *testing.T) {
	props := schemaPropertiesForType[GetLogsArgs](t)

	field, ok := props["lookback_minutes"].(map[string]interface{})
	if !ok {
		t.Fatalf("lookback_minutes not found in schema properties")
	}

	description, ok := field["description"].(string)
	if !ok {
		t.Fatalf("lookback_minutes.description expected string, got %T", field["description"])
	}

	if !strings.Contains(description, "default: 5") {
		t.Fatalf("lookback_minutes.description should advertise default: 5, got %q", description)
	}
	if strings.Contains(description, "default: 60") {
		t.Fatalf("lookback_minutes.description should not advertise default: 60, got %q", description)
	}
}

// TestGetLogsInputSchema_Structure verifies the hand-crafted GetLogsInputSchema
// conforms to the expected shape: logjson_query required, anyOf stages,
// root has additionalProperties:false (stage level does not).
func TestGetLogsInputSchema_Structure(t *testing.T) {
	schema := GetLogsInputSchema()

	// logjson_query must be in required.
	required, ok := schema["required"].([]string)
	if !ok {
		t.Fatal("schema required is not []string")
	}
	foundRequired := false
	for _, r := range required {
		if r == "logjson_query" {
			foundRequired = true
			break
		}
	}
	if !foundRequired {
		t.Error("logjson_query must be in required")
	}

	// logjson_query must have items.anyOf with at least 4 stage schemas.
	props, ok := schema["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("schema properties is not a map")
	}
	lq, ok := props["logjson_query"].(map[string]interface{})
	if !ok {
		t.Fatal("logjson_query property missing")
	}
	items, ok := lq["items"].(map[string]interface{})
	if !ok {
		t.Fatal("logjson_query items not an object")
	}
	anyOf, ok := items["anyOf"].([]interface{})
	if !ok {
		t.Fatal("logjson_query items.anyOf missing or not a slice")
	}
	if len(anyOf) < 4 {
		t.Errorf("expected at least 4 stage schemas in anyOf, got %d", len(anyOf))
	}

	// Find window_aggregate stage schema and verify its required fields.
	// Stage-level additionalProperties: false is intentionally absent so handler
	// tips (e.g. for window_minutes misuse) are reachable after schema validation.
	found := false
	for _, stageRaw := range anyOf {
		stageMap, ok := stageRaw.(map[string]interface{})
		if !ok {
			continue
		}
		stageProps, ok := stageMap["properties"].(map[string]interface{})
		if !ok {
			continue
		}
		typeField, ok := stageProps["type"].(map[string]interface{})
		if !ok {
			continue
		}
		enum, ok := typeField["enum"].([]string)
		if !ok {
			continue
		}
		for _, e := range enum {
			if e == "window_aggregate" {
				found = true
				// Stage-level additionalProperties: false is intentionally removed.
				if stageMap["additionalProperties"] == false {
					t.Error("window_aggregate stage schema must NOT have additionalProperties: false at stage level (handler tips must be reachable)")
				}
				// Schema only requires "type"; validate owns function/as/window tips.
				waRequired, ok := stageMap["required"].([]string)
				if !ok {
					t.Error("window_aggregate required is not []string")
					break
				}
				requiredSet := map[string]bool{}
				for _, r := range waRequired {
					requiredSet[r] = true
				}
				if !requiredSet["type"] {
					t.Error("window_aggregate required must include \"type\"")
				}
				for _, field := range []string{"function", "as", "window"} {
					if requiredSet[field] {
						t.Errorf("window_aggregate schema must not require %q (validate owns tips)", field)
					}
				}
				break
			}
		}
	}
	if !found {
		t.Error("window_aggregate stage schema not found in anyOf")
	}

	if schema["additionalProperties"] != false {
		t.Error("root GetLogsInputSchema must set additionalProperties: false")
	}
}

func TestGetLogsInputSchema_RejectsUnknownTopLevelKeys(t *testing.T) {
	raw, err := json.Marshal(GetLogsInputSchema())
	if err != nil {
		t.Fatalf("marshal schema: %v", err)
	}
	var parsed jsonschema.Schema
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("unmarshal schema: %v", err)
	}
	resolved, err := parsed.Resolve(nil)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	validPipeline := []interface{}{
		map[string]interface{}{
			"type":  "filter",
			"query": map[string]interface{}{"$and": []interface{}{map[string]interface{}{"$eq": []interface{}{"SeverityText", "ERROR"}}}},
		},
	}

	err = resolved.Validate(map[string]interface{}{
		"logjson_query":  validPipeline,
		"window_minutes": 5,
	})
	if err == nil {
		t.Fatal("expected top-level window_minutes to be rejected by additionalProperties:false")
	}

	// One valid document per stage type must pass schema validation.
	validStages := []struct {
		name  string
		stage map[string]interface{}
	}{
		{
			name: "filter",
			stage: map[string]interface{}{
				"type":  "filter",
				"query": map[string]interface{}{"$and": []interface{}{map[string]interface{}{"$eq": []interface{}{"SeverityText", "ERROR"}}}},
			},
		},
		{
			name: "parse without field",
			stage: map[string]interface{}{
				"type":   "parse",
				"parser": "json",
			},
		},
		{
			name: "aggregate",
			stage: map[string]interface{}{
				"type": "aggregate",
				"aggregates": []interface{}{
					map[string]interface{}{
						"function": map[string]interface{}{"$count": []interface{}{}},
						"as":       "log_count",
					},
				},
			},
		},
		{
			name: "window_aggregate",
			stage: map[string]interface{}{
				"type":     "window_aggregate",
				"function": map[string]interface{}{"$count": []interface{}{}},
				"as":       "errors",
				"window":   []interface{}{"1", "minutes"},
			},
		},
	}
	for _, tt := range validStages {
		t.Run(tt.name+" valid document passes", func(t *testing.T) {
			err := resolved.Validate(map[string]interface{}{
				"logjson_query": []interface{}{tt.stage},
			})
			if err != nil {
				t.Errorf("valid %s stage document should pass schema validation: %v", tt.name, err)
			}
		})
	}

	// Flagship wrong-key mistakes must pass schema so handler tips are reachable.
	tipReachable := []struct {
		name  string
		stage map[string]interface{}
	}{
		{
			name: "window_minutes instead of window",
			stage: map[string]interface{}{
				"type":           "window_aggregate",
				"function":       map[string]interface{}{"$count": []interface{}{}},
				"as":             "errors",
				"window_minutes": 1,
			},
		},
		{
			name: "format instead of parser",
			stage: map[string]interface{}{
				"type":   "parse",
				"format": "json",
				"field":  "Body",
			},
		},
		{
			name: "aggregates on window_aggregate",
			stage: map[string]interface{}{
				"type": "window_aggregate",
				"aggregates": []interface{}{
					map[string]interface{}{"function": map[string]interface{}{"$count": []interface{}{}}, "as": "c"},
				},
				"as":     "errors",
				"window": []interface{}{"1", "minutes"},
			},
		},
		{
			name: "conditions instead of query on filter",
			stage: map[string]interface{}{
				"type":       "filter",
				"conditions": map[string]interface{}{"$and": []interface{}{}},
			},
		},
		{
			name: "aggs instead of aggregates on aggregate",
			stage: map[string]interface{}{
				"type": "aggregate",
				"aggs": []interface{}{
					map[string]interface{}{"function": map[string]interface{}{"$count": []interface{}{}}, "as": "c"},
				},
			},
		},
		{
			name: "alias instead of as in aggregate item",
			stage: map[string]interface{}{
				"type": "aggregate",
				"aggregates": []interface{}{
					map[string]interface{}{
						"function": map[string]interface{}{"$count": []interface{}{}},
						"alias":    "count",
					},
				},
			},
		},
	}
	for _, tt := range tipReachable {
		t.Run(tt.name+" passes schema", func(t *testing.T) {
			err := resolved.Validate(map[string]interface{}{
				"logjson_query": []interface{}{tt.stage},
			})
			if err != nil {
				t.Errorf("%s should pass schema so validate tips are reachable: %v", tt.name, err)
			}
		})
	}
}
