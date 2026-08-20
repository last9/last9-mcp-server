package main

import (
	"bytes"
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"

	"last9-mcp/internal/apm"
	"last9-mcp/internal/prompts"
	"last9-mcp/internal/toolsets"
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
	// get_logs: budget covers combined (description + inputSchema JSON) served size
	// because get_logs ships a hand-crafted schema that contributes meaningfully to
	// the context window. Budget is 7000 with headroom over the baseline ~6283.
	logsIdx := byName["get_logs"]
	logsSchemaBytes, err := json.Marshal(out.Tools[logsIdx].InputSchema)
	if err != nil {
		t.Fatalf("marshal get_logs inputSchema: %v", err)
	}
	logsServedSize := len(out.Tools[logsIdx].Description) + len(logsSchemaBytes)
	const logsServedBudget = 7000
	if logsServedSize > logsServedBudget {
		t.Fatalf("get_logs combined (description + inputSchema) served size %d exceeds %d-char budget", logsServedSize, logsServedBudget)
	}
	if !strings.Contains(out.Tools[logsIdx].Description, "last9://reference/") {
		t.Fatal("get_logs description missing resource URI pointer")
	}

	// Description-only budgets for other whales (main tripwire values restored to 2000).
	whaleBudgets := map[string]int{
		"get_traces":       2000,
		"get_service_logs": 2000,
	}
	for _, whale := range []string{"get_traces", "get_service_logs"} {
		budget := whaleBudgets[whale]
		if len(out.Tools[byName[whale]].Description) > budget {
			t.Fatalf("%s served description length %d exceeds %d-char budget", whale, len(out.Tools[byName[whale]].Description), budget)
		}
		if !strings.Contains(out.Tools[byName[whale]].Description, "last9://reference/") {
			t.Fatalf("%s description missing resource URI pointer", whale)
		}
	}
	if !strings.Contains(out.Tools[byName["prometheus_range_query"]].Description, "last9://reference/metrics") {
		t.Fatal("prometheus_range_query description missing metrics resource URI pointer")
	}

	logsDesc := out.Tools[byName["get_logs"]].Description
	if strings.Contains(logsDesc, "window_minutes") {
		t.Fatal("get_logs description must not teach window_minutes; window_aggregate uses function/as/window")
	}
	for _, needle := range []string{`"function"`, `"as"`, `"window"`} {
		if !strings.Contains(logsDesc, needle) {
			t.Fatalf("get_logs description missing last9/api window_aggregate key %s", needle)
		}
	}

	tracesDesc := out.Tools[byName["get_traces"]].Description
	if strings.Contains(tracesDesc, "window_minutes") {
		t.Fatal("get_traces description must not teach window_minutes; window_aggregate uses function/as/window")
	}
	for _, needle := range []string{`"function"`, `"as"`, `"window"`} {
		if !strings.Contains(tracesDesc, needle) {
			t.Fatalf("get_traces description missing last9/api window_aggregate key %s", needle)
		}
	}
	if strings.Contains(tracesDesc, "default **5**") {
		t.Fatal("get_traces lookback default must match GetTracesArgs (60), not 5")
	}
	if !strings.Contains(tracesDesc, "default **60**") {
		t.Fatal("get_traces description missing lookback default 60")
	}

	svcLogsDesc := out.Tools[byName["get_service_logs"]].Description
	if strings.Contains(svcLogsDesc, "Prefer `get_logs` instead when") {
		t.Fatal("get_service_logs must not tell agents to prefer get_logs for HTTP status")
	}
	if strings.Contains(strings.ToLower(svcLogsDesc), "use `get_logs` + discovered status") {
		t.Fatal("get_service_logs must not send HTTP-status search to get_logs")
	}
	if !strings.Contains(svcLogsDesc, "http_status") {
		t.Fatal("get_service_logs description must document HTTP status filters")
	}

	excIdx, ok := byName["get_exceptions"]
	if !ok {
		t.Fatal("tool \"get_exceptions\" missing from dump")
	}
	excDesc := out.Tools[excIdx].Description
	if !strings.Contains(excDesc, "get_service_logs") || !strings.Contains(excDesc, "http_status") {
		t.Fatal("get_exceptions must route HTTP-status log search to get_service_logs")
	}
	if strings.Contains(excDesc, "write a `get_logs` pipeline") && !strings.Contains(excDesc, "Do not write a `get_logs` pipeline") {
		t.Fatal("get_exceptions must not send HTTP-status log search to get_logs")
	}

	if !strings.Contains(logsDesc, "get_service_logs") {
		t.Fatal("get_logs whale must name get_service_logs as the structured HTTP-status alternative")
	}

	perfIdx, ok := byName["get_service_performance_details"]
	if !ok {
		t.Fatal("tool \"get_service_performance_details\" missing from dump")
	}
	perfDesc := out.Tools[perfIdx].Description
	if strings.Contains(perfDesc, "get_service_operation_details") {
		t.Fatal("get_service_performance_details names nonexistent get_service_operation_details")
	}
	if !strings.Contains(perfDesc, "get_service_operations_summary") {
		t.Fatal("get_service_performance_details must point at get_service_operations_summary")
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
	for _, want := range []string{"get_logs", "get_traces", "prometheus_instant_query", "did_you_mean", "get_service_profile", "list_datasources"} {
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

func TestOnCallRunbookRoutesHTTPStatusToServiceLogs(t *testing.T) {
	runbook := prompts.OnCallRunbookWorkflow
	if !strings.Contains(runbook, "get_service_logs") {
		t.Fatal("on_call_runbook must name get_service_logs for status-class log search")
	}
	if !strings.Contains(runbook, "http_status_class") {
		t.Fatal("on_call_runbook must send HTTP-status log search to get_service_logs, not logjson")
	}
	if strings.Contains(runbook, "write logjson") && !strings.Contains(runbook, "do not write logjson") {
		t.Fatal("on_call_runbook must not tell the agent to write logjson for service 5xx")
	}
}
