package main

import (
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var typeSubstitutes = []string{"anyOf", "oneOf", "allOf", "$ref", "enum", "const"}

// Strict function-calling providers (OpenAI, Gemini) reject an object schema without
// "properties" and a property or array item without a "type", though both are valid
// JSON Schema. Only keys are added; existing ones are never changed.
func normalizeSchema(s map[string]any, needsType bool) {
	if needsType && s["type"] == nil && !hasKey(s, typeSubstitutes) {
		switch {
		case s["items"] != nil:
			s["type"] = "array"
		default:
			s["type"] = "object"
		}
	}
	if s["type"] == "object" && s["properties"] == nil {
		s["properties"] = map[string]any{}
	}
	if props, ok := s["properties"].(map[string]any); ok {
		for _, v := range props {
			if sub, ok := v.(map[string]any); ok {
				normalizeSchema(sub, true)
			}
		}
	}
	for _, key := range []string{"items", "additionalProperties"} {
		if sub, ok := s[key].(map[string]any); ok {
			normalizeSchema(sub, true)
		}
	}
	for _, key := range []string{"anyOf", "oneOf", "allOf"} {
		branches, _ := s[key].([]any)
		for _, b := range branches {
			if sub, ok := b.(map[string]any); ok {
				normalizeSchema(sub, false)
			}
		}
	}
	if defs, ok := s["$defs"].(map[string]any); ok {
		for _, v := range defs {
			if sub, ok := v.(map[string]any); ok {
				normalizeSchema(sub, false)
			}
		}
	}
}

func hasKey(s map[string]any, keys []string) bool {
	for _, k := range keys {
		if _, ok := s[k]; ok {
			return true
		}
	}
	return false
}

// normalizeToolInputSchema replaces tool's input schema, hand-built or inferred
// from In, with its normalized form before the SDK registers it.
func normalizeToolInputSchema[In any](tool *mcp.Tool) error {
	schema, err := inputSchemaFor[In](tool)
	if err != nil {
		return err
	}
	source := any(schema)
	if tool.InputSchema != nil {
		source = tool.InputSchema
	}
	data, err := json.Marshal(source)
	if err != nil {
		return err
	}
	var normalized map[string]any
	if err := json.Unmarshal(data, &normalized); err != nil {
		return fmt.Errorf("input schema is not an object: %w", err)
	}
	normalizeSchema(normalized, false)
	tool.InputSchema = normalized
	return nil
}
