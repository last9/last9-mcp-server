package main

import (
	"bytes"
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"

	"last9-mcp/internal/apm"
	"last9-mcp/internal/toolsets"
	"last9-mcp/internal/writeintent"
)

func TestDumpTools(t *testing.T) {
	var buf bytes.Buffer
	if err := dumpTools(&buf, nil); err != nil {
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

	summary := out.Tools[byName["get_service_summary"]]
	if strings.Contains(summary.Description, "ErrorRate") {
		t.Fatal("get_service_summary description still mentions ErrorRate")
	}
	if strings.Contains(summary.Description, "last9://reference/") {
		t.Fatal("get_service_summary must not be a whale with a last9://reference/ pointer")
	}
	if !strings.Contains(summary.Description, "http_5xx_count") {
		t.Fatal("get_service_summary description missing http_5xx_count language map")
	}
	summarySchema, err := json.Marshal(summary.InputSchema)
	if err != nil {
		t.Fatalf("marshal get_service_summary inputSchema: %v", err)
	}
	var summaryProps struct {
		Properties map[string]any `json:"properties"`
	}
	if err := json.Unmarshal(summarySchema, &summaryProps); err != nil {
		t.Fatalf("unmarshal get_service_summary inputSchema: %v", err)
	}
	for _, name := range []string{"sort_by", "limit"} {
		if _, ok := summaryProps.Properties[name]; !ok {
			t.Fatalf("get_service_summary schema missing %q", name)
		}
	}
	sortBy, _ := summaryProps.Properties["sort_by"].(map[string]any)
	enum, _ := sortBy["enum"].([]any)
	wantKeys := []string{"request_count", "throughput_rpm", "http_4xx_count", "http_5xx_count", "grpc_error_count"}
	if len(enum) != len(wantKeys) {
		t.Fatalf("sort_by enum = %#v, want %v", enum, wantKeys)
	}
	for i, want := range wantKeys {
		got, _ := enum[i].(string)
		if got != want {
			t.Fatalf("sort_by enum[%d] = %q, want %q (full=%#v)", i, got, want, enum)
		}
	}
	limitProp, _ := summaryProps.Properties["limit"].(map[string]any)
	if _, ok := limitProp["minimum"]; ok {
		t.Fatalf("limit must not set schema minimum (handler defaults 0); got %#v", limitProp["minimum"])
	}
	if _, ok := limitProp["maximum"]; ok {
		t.Fatalf("limit must not set schema maximum (handler clamps); got %#v", limitProp["maximum"])
	}

	// Org attribute catalogs must never appear as {{labels}} placeholders.
	if strings.Contains(out.Tools[byName["get_logs"]].Description, "{{labels}}") {
		t.Fatal("get_logs description still contains unsubstituted {{labels}} placeholder")
	}
	for _, whale := range []string{"get_logs", "get_traces", "get_service_logs"} {
		if len(out.Tools[byName[whale]].Description) > 2000 {
			t.Fatalf("%s served description length %d exceeds 2000-char tripwire", whale, len(out.Tools[byName[whale]].Description))
		}
		if !strings.Contains(out.Tools[byName[whale]].Description, "last9://reference/") {
			t.Fatalf("%s description missing resource URI pointer", whale)
		}
	}
	if !strings.Contains(out.Tools[byName["prometheus_range_query"]].Description, "last9://reference/metrics") {
		t.Fatal("prometheus_range_query description missing metrics resource URI pointer")
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
	if _, exists := served["allOf"]; exists {
		t.Fatal("served schema must omit provider-incompatible allOf")
	}
}

func TestDumpToolsInvestigate(t *testing.T) {
	allowed, err := toolsets.Parse("investigate")
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := dumpTools(&buf, allowed); err != nil {
		t.Fatalf("dumpTools failed: %v", err)
	}
	var out struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	byName := make(map[string]bool, len(out.Tools))
	for _, tool := range out.Tools {
		byName[tool.Name] = true
	}
	for _, want := range []string{"get_logs", "get_traces", "prometheus_instant_query", "did_you_mean", "list_datasources"} {
		if !byName[want] {
			t.Errorf("investigate dump missing %q", want)
		}
	}
	for _, deny := range []string{"get_alerts", "list_dashboards", "create_dashboard", "add_drop_rule", "list_dashboard_snapshots"} {
		if byName[deny] {
			t.Errorf("investigate dump should exclude %q", deny)
		}
	}
	if len(out.Tools) >= 38 {
		t.Fatalf("investigate should expose fewer than full surface; got %d", len(out.Tools))
	}
}

func TestDumpToolsDashboardWriteSteer(t *testing.T) {
	var buf bytes.Buffer
	if err := dumpTools(&buf, nil); err != nil {
		t.Fatalf("dumpTools failed: %v", err)
	}
	var out struct {
		Tools []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	byName := make(map[string]string, len(out.Tools))
	for _, tool := range out.Tools {
		byName[tool.Name] = tool.Description
	}
	create := byName[writeintent.Dashboard.CreateTool]
	update := byName[writeintent.Dashboard.UpdateTool]
	if create == "" || update == "" {
		t.Fatalf("dump missing %s or %s", writeintent.Dashboard.CreateTool, writeintent.Dashboard.UpdateTool)
	}
	for _, p := range writeintent.CreateDescriptionPhrases(writeintent.Dashboard) {
		if !strings.Contains(create, p.Text) {
			t.Errorf("served %s missing %q: %s", writeintent.Dashboard.CreateTool, p.Text, p.Reason)
		}
	}
	for _, p := range writeintent.UpdateDescriptionPhrases(writeintent.Dashboard) {
		if !strings.Contains(update, p.Text) {
			t.Errorf("served %s missing %q: %s", writeintent.Dashboard.UpdateTool, p.Text, p.Reason)
		}
	}
	for _, p := range writeintent.ForbiddenCreatePhrases() {
		if strings.Contains(create, p.Text) {
			t.Errorf("served %s must not contain %q: %s", writeintent.Dashboard.CreateTool, p.Text, p.Reason)
		}
	}
}
