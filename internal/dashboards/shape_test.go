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
	if !strings.HasPrefix(err.Error(), "dashboard does not match the dashboard schema; nothing was written:\n") {
		t.Errorf("error prefix: %v", err)
	}
}

func TestCheckDashboardArguments_AcceptsValidAndLeavesOtherShapesToSchema(t *testing.T) {
	for name, args := range map[string]string{
		"valid":            `{"dashboard":` + string(validCreateArgs().Dashboard) + `}`,
		"no panels":        `{"dashboard":{"name":"x","panels":[]}}`,
		"extra panel keys": `{"dashboard":{"name":"x","panels":[{"id":"p","name":"p","description":"d","unit":"s","layout":{"x":0,"y":0,"w":6,"h":6},"visualization":{"type":"section"}}]}}`,
		"title with name":  `{"dashboard":{"name":"x","panels":[{"name":"p","title":"p","layout":{"x":0,"y":0,"w":6,"h":6},"visualization":{"type":"markdown"}}]}}`,
		"extra dashboard and query keys": `{"dashboard":{"name":"x","foo":1,"panels":[{"name":"p","layout":{"x":0,"y":0,"w":6,"h":6},"visualization":{"type":"stat"},` +
			`"queries":[{"name":"A","type":"range","expr":"1","telemetry":"metrics","query_type":"promql","legend":{"type":"auto","value":""},"foo":1}]}]}}`,
		"no dashboard":     `{}`,
		"dashboard string": `{"dashboard":"x"}`,
		"dashboard null":   `{"dashboard":null}`,
		"not an object":    `[]`,
		"not json":         `{`,
	} {
		if err := CheckDashboardArguments(json.RawMessage(args)); err != nil {
			t.Errorf("%s: unexpected error %v", name, err)
		}
	}
}

func TestCheckDashboardArguments_NamesWrongContainerTypes(t *testing.T) {
	for args, want := range map[string]string{
		`{"dashboard":{"name":"x","panels":{}}}`:     "dashboard.panels: got object, want array",
		`{"dashboard":{"name":"x","panels":[null]}}`: "dashboard.panels[0]: got null, want object {name, layout, visualization}",
	} {
		err := CheckDashboardArguments(json.RawMessage(args))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: want %q, got %v", args, want, err)
		}
	}
}

func TestShapeCheck_IntegerFieldsRejectOnlyFractionalNumbers(t *testing.T) {
	panel := func(rowLimit string) map[string]any {
		var p map[string]any
		_ = json.Unmarshal([]byte(`{"visualization":{"type":"logs","logs_config":{"columns":["body"],"row_limit":`+rowLimit+`}}}`), &p)
		return p
	}
	want := "panel.visualization.logs_config.row_limit: got number, want integer"
	if got := checkPanelShape(panel("1.5"), "panel"); strings.Join(got, "\n") != want {
		t.Errorf("fractional row_limit: got %q, want %q", got, want)
	}
	for _, whole := range []string{"100", "100.0", "1e2"} {
		if got := checkPanelShape(panel(whole), "panel"); len(got) != 0 {
			t.Errorf("row_limit %s: unexpected %v", whole, got)
		}
	}
}

func TestCheckDashboardArguments_NamesConditionallyRequiredFields(t *testing.T) {
	logsPanel := `{"name":"a","layout":{"x":0,"y":0,"w":6,"h":6},"visualization":{"type":"logs"},"queries":[]}`
	noQueriesPanel := `{"name":"b","layout":{"x":0,"y":0,"w":6,"h":6},"visualization":{"type":"stat"}}`
	err := CheckDashboardArguments(json.RawMessage(`{"dashboard":{"name":"x","panels":[` + logsPanel + "," + logsPanel + "," + noQueriesPanel + `]}}`))
	if err == nil {
		t.Fatal("expected shape error")
	}
	for _, want := range []string{
		"dashboard.panels[0].visualization.logs_config: missing; want object {columns}",
		"dashboard.panels[1].visualization.logs_config: missing; want object {columns}",
		"dashboard.panels[2]: want one of visualization.type section, visualization.type markdown, csv, queries",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error lacks %q:\n%v", want, err)
		}
	}
	for _, other := range []string{"panels[0]: want one of", "panels[2].visualization.logs_config"} {
		if strings.Contains(err.Error(), other) {
			t.Errorf("error reports a satisfied condition %q:\n%v", other, err)
		}
	}
	if got := checkPanelShape(map[string]any{"visualization": map[string]any{"type": "logs"}}, "panel"); len(got) != 0 {
		t.Fatalf("partial patch panel: %v", got)
	}
}

func TestShapeCheck_NamesMistypedFieldsInsideAlternatives(t *testing.T) {
	csvPanel := `{"name":"a","layout":{"x":0,"y":0,"w":6,"h":6},"visualization":{"type":"stat"},"csv":"bad"}`
	err := CheckDashboardArguments(json.RawMessage(`{"dashboard":{"name":"x","panels":[` + csvPanel + "," + csvPanel + `]}}`))
	if err == nil {
		t.Fatal("expected shape error")
	}
	for _, want := range []string{"dashboard.panels[0].csv: got string, want object", "dashboard.panels[1].csv: got string, want object"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error lacks %q:\n%v", want, err)
		}
	}
	if strings.Contains(err.Error(), "want one of") {
		t.Errorf("error lists alternatives when the chosen one is mistyped:\n%v", err)
	}
	var patch map[string]any
	_ = json.Unmarshal([]byte(`{"visualization":{"type":"stat"},"csv":"bad"}`), &patch)
	if got := strings.Join(checkPanelShape(patch, "panel"), "\n"); got != "panel.csv: got string, want object" {
		t.Errorf("patch panel: got %q", got)
	}
}

func TestShapeCheck_NamesValuesOutsideEnumsAndBounds(t *testing.T) {
	var panel map[string]any
	_ = json.Unmarshal([]byte(`{"version":2,"visualization":{"type":"logs","logs_config":{"columns":["body","nope"],"row_limit":0,"sort_order":"up"}}}`), &panel)
	want := []string{
		`panel.version: got 2, want integer from 1 to 1`,
		`panel.visualization.logs_config.columns[1]: got "nope", want one of timestamp, reason, severity, kind, service, body`,
		`panel.visualization.logs_config.row_limit: got 0, want integer from 1 to 1000`,
		`panel.visualization.logs_config.sort_order: got "up", want one of asc, desc`,
	}
	if got := checkPanelShape(panel, "panel"); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestCheckDashboardArguments_CapsProblems(t *testing.T) {
	// Each panel is valid except for a string legend: exactly one problem per panel.
	oneProblemPanel := `{"name":"p","version":1,"layout":{"x":0,"y":0,"w":6,"h":6},"visualization":{"type":"stat"},"queries":[{"name":"A","type":"range","expr":"1","telemetry":"metrics","query_type":"promql","legend":"l"}]}`
	for _, tc := range []struct {
		problems int
		lastLine string
	}{
		{maxShapeProblems - 1, "- dashboard.panels[18].queries[0].legend: got string, want object {type, value}"},
		{maxShapeProblems, "- dashboard.panels[19].queries[0].legend: got string, want object {type, value}"},
		{maxShapeProblems + 1, "- and 1 more"},
	} {
		panels := make([]string, tc.problems)
		for i := range panels {
			panels[i] = oneProblemPanel
		}
		err := CheckDashboardArguments(json.RawMessage(`{"dashboard":{"name":"x","panels":[` + strings.Join(panels, ",") + `]}}`))
		if err == nil {
			t.Fatalf("%d problems: expected shape error", tc.problems)
		}
		lines := strings.Split(err.Error(), "\n")
		wantLines := 1 + min(tc.problems, maxShapeProblems+1)
		if len(lines) != wantLines || lines[len(lines)-1] != tc.lastLine {
			t.Errorf("%d problems: got %d lines (want %d), last %q (want %q)", tc.problems, len(lines), wantLines, lines[len(lines)-1], tc.lastLine)
		}
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

func TestShapeCheck_FlagsAliasesOnlyWhereTheirFieldIsNeeded(t *testing.T) {
	layout := `"layout":{"x":0,"y":0,"w":6,"h":6}`
	sectionPanel := `{"name":"s",` + layout + `,"visualization":{"type":"section"},"targets":[]}`
	markdownPanel := `{"name":"m",` + layout + `,"visualization":{"type":"markdown"},"targets":[]}`
	csvPanel := `{"name":"c",` + layout + `,"visualization":{"type":"table"},"csv":{"columns":{}},"targets":[]}`
	if err := CheckDashboardArguments(json.RawMessage(`{"dashboard":{"name":"x","panels":[` + sectionPanel + "," + markdownPanel + "," + csvPanel + `]}}`)); err != nil {
		t.Errorf("targets beside a satisfied alternative: unexpected %v", err)
	}
	for _, patch := range []string{
		`{"visualization":{"type":"section"},"targets":[]}`,
		`{"visualization":{"type":"markdown"},"targets":[]}`,
		`{"visualization":{"type":"table"},"csv":{"columns":{}},"targets":[]}`,
	} {
		var panel map[string]any
		_ = json.Unmarshal([]byte(patch), &panel)
		if got := checkPanelShape(panel, "panel"); len(got) != 0 {
			t.Errorf("patch panel %s: unexpected %v", patch, got)
		}
	}

	statPanel := `{"name":"t",` + layout + `,"visualization":{"type":"stat"},"targets":[]}`
	err := CheckDashboardArguments(json.RawMessage(`{"dashboard":{"name":"x","panels":[` + statPanel + `]}}`))
	if want := "dashboard.panels[0].targets: unknown panel field; use queries"; err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("targets standing in for queries: want %q, got %v", want, err)
	}
	var stat map[string]any
	_ = json.Unmarshal([]byte(`{"visualization":{"type":"stat"},"targets":[]}`), &stat)
	if got := strings.Join(checkPanelShape(stat, "panel"), "\n"); got != "panel.targets: unknown panel field; use queries" {
		t.Errorf("patch stat panel with targets: got %q", got)
	}

	// Without visualization the section and markdown alternatives hold vacuously; they
	// must not hide targets on a panel that will need queries once it names its type.
	grafanaPanel := `{"title":"p","type":"timeseries","gridPos":{"x":0,"y":0,"w":6,"h":6},"targets":[{"expr":"up"}]}`
	grafanaFields := []string{
		"gridPos: unknown panel field; use layout {x, y, w, h}",
		"targets: unknown panel field; use queries",
		"title: unknown panel field; use name",
		"type: unknown panel field; use visualization {type}",
	}
	err = CheckDashboardArguments(json.RawMessage(`{"dashboard":{"name":"x","panels":[` + grafanaPanel + `]}}`))
	for _, field := range grafanaFields {
		if want := "dashboard.panels[0]." + field; err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Grafana panel without visualization: want %q in one call, got %v", want, err)
		}
	}
	var grafana map[string]any
	_ = json.Unmarshal([]byte(grafanaPanel), &grafana)
	got := checkPanelShape(grafana, "panel")
	want := make([]string, len(grafanaFields))
	for i, field := range grafanaFields {
		want[i] = "panel." + field
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("patch Grafana panel without visualization: got %q, want %q", got, want)
	}
}
