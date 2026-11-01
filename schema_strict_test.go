package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"last9-mcp/internal/toolsets"
)

// strictGaps lists what OpenAI/Gemini-style function calling rejects: objects
// without properties, and properties or array items without a type.
func strictGaps(s map[string]any, path string, needsType bool) []string {
	var gaps []string
	if needsType && s["type"] == nil && !hasKey(s, typeSubstitutes) {
		gaps = append(gaps, path+": no type")
	}
	if s["type"] == "object" && s["properties"] == nil {
		gaps = append(gaps, path+": object without properties")
	}
	if props, ok := s["properties"].(map[string]any); ok {
		for k, v := range props {
			if sub, ok := v.(map[string]any); ok {
				gaps = append(gaps, strictGaps(sub, path+"."+k, true)...)
			}
		}
	}
	if sub, ok := s["items"].(map[string]any); ok {
		gaps = append(gaps, strictGaps(sub, path+"[]", true)...)
	}
	if sub, ok := s["additionalProperties"].(map[string]any); ok {
		gaps = append(gaps, strictGaps(sub, path+".*", true)...)
	}
	for _, key := range []string{"anyOf", "oneOf", "allOf"} {
		branches, _ := s[key].([]any)
		for i, b := range branches {
			if sub, ok := b.(map[string]any); ok {
				gaps = append(gaps, strictGaps(sub, fmt.Sprintf("%s/%s%d", path, key, i), false)...)
			}
		}
	}
	if defs, ok := s["$defs"].(map[string]any); ok {
		for k, v := range defs {
			if sub, ok := v.(map[string]any); ok {
				gaps = append(gaps, strictGaps(sub, path+"$defs."+k, false)...)
			}
		}
	}
	return gaps
}

func TestServedToolSchemasAreStrictProviderSafe(t *testing.T) {
	var buf bytes.Buffer
	if err := dumpTools(&buf, toolsets.Filter{}); err != nil {
		t.Fatalf("dumpTools: %v", err)
	}
	var out struct {
		Tools []struct {
			Name        string         `json:"name"`
			InputSchema map[string]any `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Tools) == 0 {
		t.Fatal("no tools served")
	}
	for _, tool := range out.Tools {
		for _, gap := range strictGaps(tool.InputSchema, "", false) {
			t.Errorf("%s%s", tool.Name, gap)
		}
	}
}

func subsumes(orig, norm any) bool {
	switch o := orig.(type) {
	case map[string]any:
		n, ok := norm.(map[string]any)
		if !ok {
			return false
		}
		for k, v := range o {
			if !subsumes(v, n[k]) {
				return false
			}
		}
		return true
	case []any:
		n, ok := norm.([]any)
		if !ok || len(n) != len(o) {
			return false
		}
		for i := range o {
			if !subsumes(o[i], n[i]) {
				return false
			}
		}
		return true
	}
	return reflect.DeepEqual(orig, norm)
}

func TestNormalizeSchemaIsAdditive(t *testing.T) {
	const raw = `{
	  "type":"object","additionalProperties":false,"required":["a"],
	  "properties":{
	    "a":{"type":"string","description":"d","enum":["x","y"]},
	    "m":{"type":"object","additionalProperties":{"type":"string"},"description":"map"},
	    "free":{"type":"object","description":"raw"},
	    "list":{"type":"array","items":{"type":"object","additionalProperties":true}},
	    "untypedItems":{"type":"array","items":{"properties":{"k":{"type":"integer"}}}},
	    "u":{"description":"untyped"},
	    "either":{"anyOf":[{"type":"object"},{"required":["q"]}]}
	  },
	  "$defs":{"d":{"type":"object"}}
	}`
	var orig, norm map[string]any
	if err := json.Unmarshal([]byte(raw), &orig); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(raw), &norm); err != nil {
		t.Fatal(err)
	}
	normalizeSchema(norm, false)

	if !subsumes(orig, norm) {
		t.Fatalf("normalization changed or removed existing schema content:\norig=%v\nnorm=%v", orig, norm)
	}
	if gaps := strictGaps(norm, "", false); len(gaps) != 0 {
		t.Fatalf("gaps remain: %v", gaps)
	}
	props := norm["properties"].(map[string]any)
	if props["u"].(map[string]any)["type"] != "object" {
		t.Errorf("untyped free-form property should become object: %v", props["u"])
	}
	if props["untypedItems"].(map[string]any)["items"].(map[string]any)["type"] != "object" {
		t.Errorf("items with properties should become object")
	}
	snapshot, err := json.Marshal(norm)
	if err != nil {
		t.Fatal(err)
	}
	var again map[string]any
	if err := json.Unmarshal(snapshot, &again); err != nil {
		t.Fatal(err)
	}
	normalizeSchema(again, false)
	if !reflect.DeepEqual(norm, again) {
		t.Error("normalizeSchema is not idempotent")
	}
}
