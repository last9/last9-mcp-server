package dashboards

import (
	"encoding/json"
	"strings"
	"testing"
)

const grafanaShapedArgs = `{"dashboard":{"name":"x","panels":[{"title":"p","type":"timeseries","gridPos":{"x":0,"y":0,"w":24,"h":8},"queries":[{"legend":"l","query":"up","type":"prometheus"}]}]},"metadata":{"_category":"custom","_type":"metrics"}}`

func TestCheckDashboardArguments_NamesEveryWrongField(t *testing.T) {
	err := CheckDashboardArguments(json.RawMessage(grafanaShapedArgs))
	if err == nil {
		t.Fatal("expected shape error")
	}
	for _, want := range []string{
		"dashboard.panels[0].gridPos: unknown panel field; use layout {x, y, w, h}",
		"dashboard.panels[0].title: unknown panel field; use name",
		"dashboard.panels[0].type: unknown panel field; use visualization {type}",
		"dashboard.panels[0].queries[0].query: unknown query field; use expr",
		"dashboard.panels[0].queries[0].legend: got string, want object {type, value}",
		"dashboard.panels[0].queries[0].name: missing; want string",
		"dashboard.panels[0].queries[0].query_type: missing; want one of promql, log_json, log_ql, log_raw, trace_json, trace_ql",
		"dashboard.panels[0].queries[0].telemetry: missing; want string",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error lacks %q:\n%v", want, err)
		}
	}
	for _, alias := range []string{"panels[0].name: missing", "panels[0].layout: missing", "panels[0].visualization: missing", "queries[0].expr: missing"} {
		if strings.Contains(err.Error(), alias) {
			t.Errorf("error repeats aliased field as missing %q:\n%v", alias, err)
		}
	}
	if !strings.HasPrefix(err.Error(), "dashboard does not match the create_dashboard schema; nothing was written:\n") {
		t.Errorf("error prefix: %v", err)
	}
}

func TestCheckDashboardArguments_AcceptsValidAndLeavesOtherShapesToSchema(t *testing.T) {
	for name, args := range map[string]string{
		"valid":            `{"dashboard":` + string(validCreateArgs().Dashboard) + `}`,
		"no panels":        `{"dashboard":{"name":"x","panels":[]}}`,
		"extra panel keys": `{"dashboard":{"name":"x","panels":[{"id":"p","name":"p","description":"d","unit":"s","layout":{"x":0,"y":0,"w":6,"h":6},"visualization":{"type":"section"}}]}}`,
		"title with name":  `{"dashboard":{"name":"x","panels":[{"name":"p","title":"p","layout":{"x":0,"y":0,"w":6,"h":6},"visualization":{"type":"markdown"}}]}}`,
		"no dashboard":     `{}`,
		"not an object":    `[]`,
		"not json":         `{`,
	} {
		if err := CheckDashboardArguments(json.RawMessage(args)); err != nil {
			t.Errorf("%s: unexpected error %v", name, err)
		}
	}
}

func TestCheckDashboardArguments_CapsProblems(t *testing.T) {
	panels := make([]string, 30)
	for i := range panels {
		panels[i] = `{"title":"p"}`
	}
	err := CheckDashboardArguments(json.RawMessage(`{"dashboard":{"name":"x","panels":[` + strings.Join(panels, ",") + `]}}`))
	if err == nil {
		t.Fatal("expected shape error")
	}
	lines := strings.Split(err.Error(), "\n")
	if len(lines) != maxShapeProblems+2 || !strings.HasPrefix(lines[len(lines)-1], "- and ") {
		t.Fatalf("got %d lines, last %q", len(lines), lines[len(lines)-1])
	}
}

func TestCheckPanelShape_FlagsAliasesAndTypesButNotMissingFields(t *testing.T) {
	var panel map[string]any
	_ = json.Unmarshal([]byte(`{"title":"p","gridPos":{"x":0},"queries":[{"legend":"l","expr":"up"}]}`), &panel)
	got := checkPanelShape(panel, "panel")
	want := []string{
		"panel.gridPos: unknown panel field; use layout {x, y, w, h}",
		"panel.queries[0].legend: got string, want object {type, value}",
		"panel.title: unknown panel field; use name",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if got := checkPanelShape(map[string]any{"id": "p3"}, "panel"); len(got) != 0 {
		t.Fatalf("minimal panel: %v", got)
	}
}
