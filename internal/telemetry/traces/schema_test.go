package traces

import (
	"encoding/json"
	"reflect"
	"slices"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
)

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

// findStageByType finds the first oneOf stage whose "type" enum contains the given value.
func findStageByType(oneOf []interface{}, stageType string) map[string]interface{} {
	for _, s := range oneOf {
		stage, ok := s.(map[string]interface{})
		if !ok {
			continue
		}
		stageProps, ok := stage["properties"].(map[string]interface{})
		if !ok {
			continue
		}
		typeField, ok := stageProps["type"].(map[string]interface{})
		if !ok {
			continue
		}
		if slices.Contains(typeField["enum"].([]string), stageType) {
			return stage
		}
	}
	return nil
}

// TestGetTracesInputSchema_Structure validates the hand-crafted InputSchema has the
// correct structure: tracejson_query with items.oneOf covering all stage types, and
// aggregate stage with additionalProperties:false to block hallucinated key names.
func TestGetTracesInputSchema_Structure(t *testing.T) {
	schema := GetTracesInputSchema()

	props, ok := schema["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("InputSchema missing 'properties'")
	}

	// tracejson_query must exist and be an array with oneOf items
	tq, ok := props["tracejson_query"].(map[string]interface{})
	if !ok {
		t.Fatalf("tracejson_query missing from properties")
	}
	if tq["type"] != "array" {
		t.Errorf("tracejson_query type: want array, got %v", tq["type"])
	}
	items, ok := tq["items"].(map[string]interface{})
	if !ok {
		t.Fatalf("tracejson_query.items missing or not an object")
	}
	oneOf, ok := items["oneOf"].([]interface{})
	if !ok {
		t.Fatalf("tracejson_query.items.oneOf missing")
	}
	// filter, aggregate, window_aggregate, parse, transform, select
	const wantStages = 6
	if len(oneOf) != wantStages {
		t.Errorf("oneOf stage count: want %d, got %d", wantStages, len(oneOf))
	}

	// required must include tracejson_query
	required, ok := schema["required"].([]string)
	if !ok {
		t.Fatalf("InputSchema missing 'required'")
	}
	if !slices.Contains(required, "tracejson_query") {
		t.Error("tracejson_query must be in required")
	}

	// aggregate stage must have additionalProperties:false to block wrong key names
	aggregateStage := findStageByType(oneOf, "aggregate")
	if aggregateStage == nil {
		t.Fatal("aggregate stage not found in oneOf")
	}
	if aggregateStage["additionalProperties"] != false {
		t.Errorf("aggregate stage must have additionalProperties:false, got %v", aggregateStage["additionalProperties"])
	}

	// aggregate entry items must also have additionalProperties:false
	aggProps, ok := aggregateStage["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("aggregate stage missing 'properties'")
	}
	aggregates, ok := aggProps["aggregates"].(map[string]interface{})
	if !ok {
		t.Fatal("aggregate stage missing 'aggregates' property")
	}
	aggItems, ok := aggregates["items"].(map[string]interface{})
	if !ok {
		t.Fatal("aggregate stage.aggregates.items missing")
	}
	if aggItems["additionalProperties"] != false {
		t.Errorf("aggregate items must have additionalProperties:false, got %v", aggItems["additionalProperties"])
	}

	// all scalar params must be present
	for _, param := range []string{"start_time_iso", "end_time_iso", "lookback_minutes", "limit"} {
		if _, ok := props[param]; !ok {
			t.Errorf("param %q missing from InputSchema properties", param)
		}
	}
}

// TestGetTracesArgs_SchemaCompatibility ensures the generated JSON Schema for
// GetTracesArgs does not contain "items": true for the tracejson_query field.
//
// OpenAI and other strict JSON Schema validators reject boolean values for
// "items" (valid only in draft-07+). The field must use []map[string]interface{}
// so the schema emits "items": {"type": "object"} instead.
func TestGetTracesArgs_SchemaCompatibility(t *testing.T) {
	schema, err := jsonschema.ForType(reflect.TypeFor[GetTracesArgs](), &jsonschema.ForOptions{})
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

	field, ok := props["tracejson_query"].(map[string]interface{})
	if !ok {
		t.Fatalf("tracejson_query not found in schema properties")
	}

	// Must include "array" in its type (may also include "null" due to omitempty).
	if !hasArrayType(field["type"]) {
		t.Errorf("tracejson_query: expected type to include array, got %v", field["type"])
	}

	// "items" must not be a boolean true — that is rejected by OpenAI and
	// other strict providers.
	items := field["items"]
	if items == nil {
		t.Fatal("tracejson_query: items is missing from schema")
	}
	if _, isBool := items.(bool); isBool {
		t.Errorf("tracejson_query: items must be an object schema, not a boolean (got %v). "+
			"This breaks OpenAI and other strict JSON Schema validators. "+
			"Fix: use []map[string]interface{} instead of []interface{}", items)
	}

	// "items" must be an object schema with type=object.
	itemsMap, ok := items.(map[string]interface{})
	if !ok {
		t.Fatalf("tracejson_query: items is not an object, got %T: %v", items, items)
	}
	if itemsMap["type"] != "object" {
		t.Errorf("tracejson_query: items.type expected=object, got %v", itemsMap["type"])
	}
}
