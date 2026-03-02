package logs

import (
	"encoding/json"
	"reflect"
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

// TestGetLogsArgs_SchemaCompatibility ensures the generated JSON Schema for
// GetLogsArgs does not contain "items": true for the logjson_query field.
//
// OpenAI and other strict JSON Schema validators reject boolean values for
// "items" (valid only in draft-07+). The field must use []map[string]interface{}
// so the schema emits "items": {"type": "object"} instead.
func TestGetLogsArgs_SchemaCompatibility(t *testing.T) {
	schema, err := jsonschema.ForType(reflect.TypeFor[GetLogsArgs](), &jsonschema.ForOptions{})
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
