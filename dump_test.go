package main

import (
	"bytes"
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"

	"last9-mcp/internal/apm"
)

func TestDumpTools(t *testing.T) {
	var buf bytes.Buffer
	if err := dumpTools(&buf); err != nil {
		t.Fatalf("dumpTools failed: %v", err)
	}

	var out struct {
		Tools []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			InputSchema any    `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	// All registered tools must be covered — the whole point of dump-tools.
	// A loose floor would let a regression silently drop tools. Tighten this
	// when the committed snapshot + CI equality gate supersedes it.
	if len(out.Tools) < 38 {
		t.Fatalf("expected at least 38 tools, got %d", len(out.Tools))
	}
	if !sort.SliceIsSorted(out.Tools, func(i, j int) bool { return out.Tools[i].Name < out.Tools[j].Name }) {
		t.Fatal("tools are not sorted by name (output must be deterministic for snapshot diffing)")
	}

	byName := make(map[string]int)
	for i, tool := range out.Tools {
		byName[tool.Name] = i
	}
	for _, name := range []string{"get_traces", "get_service_summary", "prometheus_label_values", "get_logs"} {
		i, ok := byName[name]
		if !ok {
			t.Fatalf("tool %q missing from dump", name)
		}
		if strings.TrimSpace(out.Tools[i].Description) == "" {
			t.Fatalf("tool %q has empty description", name)
		}
		if out.Tools[i].InputSchema == nil {
			t.Fatalf("tool %q has no inputSchema", name)
		}
	}

	// The {{labels}} placeholder must never leak into served descriptions —
	// enhancement substitutes it (empty on a cold cache).
	if strings.Contains(out.Tools[byName["get_logs"]].Description, "{{labels}}") {
		t.Fatal("get_logs description still contains unsubstituted {{labels}} placeholder")
	}

	deviationsIndex, ok := byName["get_apm_service_deviations"]
	if !ok {
		t.Fatal("tool \"get_apm_service_deviations\" missing from dump")
	}
	deviations := out.Tools[deviationsIndex]
	if strings.TrimSpace(deviations.Description) == "" {
		t.Fatal("get_apm_service_deviations has an empty description")
	}
	servedSchema, err := json.Marshal(deviations.InputSchema)
	if err != nil {
		t.Fatalf("marshal get_apm_service_deviations inputSchema: %v", err)
	}
	var served map[string]interface{}
	if err := json.Unmarshal(servedSchema, &served); err != nil {
		t.Fatalf("unmarshal served get_apm_service_deviations inputSchema: %v", err)
	}
	wantBytes, err := json.Marshal(apm.GetAPMServiceDeviationsInputSchema())
	if err != nil {
		t.Fatalf("marshal canonical get_apm_service_deviations inputSchema: %v", err)
	}
	var want map[string]interface{}
	if err := json.Unmarshal(wantBytes, &want); err != nil {
		t.Fatalf("unmarshal canonical get_apm_service_deviations inputSchema: %v", err)
	}
	if !reflect.DeepEqual(served, want) {
		t.Fatalf("served get_apm_service_deviations schema differs from canonical schema\nserved: %#v\nwant: %#v", served, want)
	}
	if served["additionalProperties"] != false {
		t.Fatalf("additionalProperties = %v, want false", served["additionalProperties"])
	}
	if _, exists := served["required"]; exists {
		t.Fatal("served schema must not have a top-level required list")
	}
	properties := served["properties"].(map[string]interface{})
	if len(properties) != 10 {
		t.Fatalf("served schema has %d properties, want 10", len(properties))
	}
	for name, value := range properties {
		property := value.(map[string]interface{})
		if description, _ := property["description"].(string); strings.TrimSpace(description) == "" {
			t.Errorf("served property %q has an empty description", name)
		}
	}
	for _, name := range []string{"start_time_iso", "end_time_iso", "baseline_start_time_iso", "baseline_end_time_iso"} {
		if properties[name].(map[string]interface{})["format"] != "date-time" {
			t.Errorf("served property %q is missing date-time format", name)
		}
	}
	for _, name := range []string{"max_services", "max_operations"} {
		property := properties[name].(map[string]interface{})
		if property["minimum"] != float64(1) || property["maximum"] != float64(10) || property["default"] != float64(10) {
			t.Errorf("served property %q has incorrect bounds/default: %#v", name, property)
		}
	}
	if properties["lookback_minutes"].(map[string]interface{})["minimum"] != float64(1) {
		t.Error("served lookback_minutes minimum must be 1")
	}
	wantDependent := map[string]interface{}{
		"start_time_iso":          []interface{}{"end_time_iso"},
		"end_time_iso":            []interface{}{"start_time_iso"},
		"baseline_start_time_iso": []interface{}{"baseline_end_time_iso"},
		"baseline_end_time_iso":   []interface{}{"baseline_start_time_iso"},
	}
	if !reflect.DeepEqual(served["dependentRequired"], wantDependent) {
		t.Fatalf("served dependentRequired = %#v, want %#v", served["dependentRequired"], wantDependent)
	}
	wantAllOf := []interface{}{
		map[string]interface{}{"not": map[string]interface{}{"required": []interface{}{"lookback_minutes", "start_time_iso"}}},
		map[string]interface{}{"not": map[string]interface{}{"required": []interface{}{"lookback_minutes", "end_time_iso"}}},
	}
	if !reflect.DeepEqual(served["allOf"], wantAllOf) {
		t.Fatalf("served allOf = %#v, want %#v", served["allOf"], wantAllOf)
	}
}
