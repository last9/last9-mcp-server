package dashboards

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const patchFixture = `{
	"dashboard":{
		"id":"dash-1","name":"Gateway",
		"variables":[{"target":"env","type":"label","source":"env"}],
		"panels":[
			{"id":"p1","name":"Latency","version":1,"visualization":{"type":"timeseries"},"queries":[{"name":"A","expr":"rate(x[1m])"}]},
			{"id":"p2","name":"Errors","version":1,"visualization":{"type":"timeseries"},"queries":[{"name":"A","expr":"rate(y[1m])"}]},
			{"id":"s1","name":"Details","visualization":{"type":"section"}}
		]
	},
	"metadata":{"_category":"custom","_type":"metrics"}
}`

type patchServer struct {
	srv  *httptest.Server
	puts []DashboardRequest
	gets []string
}

func newPatchServer(t *testing.T) *patchServer {
	t.Helper()
	return newPatchServerServing(t, patchFixture)
}

func newPatchServerServing(t *testing.T, fixture string) *patchServer {
	t.Helper()
	ps := &patchServer{}
	ps.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/dashboards/dash-1" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		switch r.Method {
		case http.MethodGet:
			ps.gets = append(ps.gets, r.URL.RawQuery)
			_, _ = io.WriteString(w, fixture)
		case http.MethodPut:
			var req DashboardRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Error(err)
			}
			ps.puts = append(ps.puts, req)
			_, _ = io.WriteString(w, `{"dashboard":{"id":"dash-1"}}`)
		default:
			t.Errorf("unexpected method %s", r.Method)
		}
	}))
	t.Cleanup(ps.srv.Close)
	return ps
}

func (ps *patchServer) patch(t *testing.T, ops string) (*mcp.CallToolResult, error) {
	t.Helper()
	var args PatchDashboardArgs
	if err := json.Unmarshal([]byte(`{"id":"dash-1","ops":`+ops+`}`), &args); err != nil {
		t.Fatal(err)
	}
	result, _, err := NewPatchDashboardHandler(ps.srv.Client(), testDashboardConfig(ps.srv.URL))(context.Background(), &mcp.CallToolRequest{}, args)
	return result, err
}

func (ps *patchServer) sentDashboard(t *testing.T) map[string]any {
	t.Helper()
	if len(ps.puts) != 1 {
		t.Fatalf("PUT count = %d, want 1", len(ps.puts))
	}
	var doc map[string]any
	if err := json.Unmarshal(ps.puts[0].Dashboard, &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func fixtureDashboard(t *testing.T) map[string]any {
	t.Helper()
	var env struct {
		Dashboard map[string]any `json:"dashboard"`
	}
	if err := json.Unmarshal([]byte(patchFixture), &env); err != nil {
		t.Fatal(err)
	}
	return env.Dashboard
}

func panelIDs(doc map[string]any) []string {
	var ids []string
	for _, p := range doc["panels"].([]any) {
		ids = append(ids, p.(map[string]any)["id"].(string))
	}
	return ids
}

func assertUntouchedExcept(t *testing.T, got map[string]any, keys ...string) {
	t.Helper()
	want := fixtureDashboard(t)
	for _, k := range keys {
		delete(want, k)
		delete(got, k)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("untargeted fields changed:\ngot  %v\nwant %v", got, want)
	}
}

func TestPatchDashboard_AddPanel(t *testing.T) {
	ps := newPatchServer(t)
	result, err := ps.patch(t, `[{"op":"add_panel","panel":{"id":"p3","name":"Saturation","visualization":{"type":"timeseries"}},"index":1}]`)
	if err != nil {
		t.Fatal(err)
	}
	doc := ps.sentDashboard(t)
	if got, want := panelIDs(doc), []string{"p1", "p3", "p2", "s1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("panels %v want %v", got, want)
	}
	added := doc["panels"].([]any)[1].(map[string]any)
	if added["version"] != float64(1) {
		t.Fatalf("added panel version %v, want defaulted to 1", added["version"])
	}
	if text := utils.GetTextContent(t, result); !strings.Contains(text, `"op":"add_panel","panel_id":"p3","index":1`) {
		t.Fatalf("diff %s", text)
	}
}

func TestPatchDashboard_AddPanelAppendsByDefault(t *testing.T) {
	ps := newPatchServer(t)
	if _, err := ps.patch(t, `[{"op":"add_panel","panel":{"id":"p3","name":"Saturation"}}]`); err != nil {
		t.Fatal(err)
	}
	if got, want := panelIDs(ps.sentDashboard(t)), []string{"p1", "p2", "s1", "p3"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("panels %v want %v", got, want)
	}
}

func TestPatchDashboard_UpdatePanel(t *testing.T) {
	ps := newPatchServer(t)
	result, err := ps.patch(t, `[{"op":"update_panel","panel_id":"p2","fields":{"name":"5xx errors","queries":[{"name":"A","expr":"rate(z[5m])"}]}}]`)
	if err != nil {
		t.Fatal(err)
	}
	doc := ps.sentDashboard(t)
	panels := doc["panels"].([]any)
	p2 := panels[1].(map[string]any)
	if p2["name"] != "5xx errors" || p2["visualization"].(map[string]any)["type"] != "timeseries" {
		t.Fatalf("p2 = %v", p2)
	}
	want := fixtureDashboard(t)
	if !reflect.DeepEqual(panels[0], want["panels"].([]any)[0]) || !reflect.DeepEqual(panels[2], want["panels"].([]any)[2]) {
		t.Fatalf("untargeted panels changed: %v", panels)
	}
	assertUntouchedExcept(t, doc, "panels")
	text := utils.GetTextContent(t, result)
	for _, s := range []string{`"op":"update_panel"`, `"panel_id":"p2"`, `"before":"Errors"`, `"after":"5xx errors"`, `rate(z[5m])`} {
		if !strings.Contains(text, s) {
			t.Fatalf("diff missing %s: %s", s, text)
		}
	}
	if strings.Contains(text, "Latency") {
		t.Fatalf("diff mentions an untouched panel: %s", text)
	}
}

func TestPatchDashboard_UpdatePanelNullRemovesField(t *testing.T) {
	ps := newPatchServer(t)
	if _, err := ps.patch(t, `[{"op":"update_panel","panel_id":"p1","fields":{"queries":null}}]`); err != nil {
		t.Fatal(err)
	}
	p1 := ps.sentDashboard(t)["panels"].([]any)[0].(map[string]any)
	if _, ok := p1["queries"]; ok {
		t.Fatalf("queries not removed: %v", p1)
	}
}

func TestPatchDashboard_UpdatePanelNullRemovesNullField(t *testing.T) {
	ps := newPatchServerServing(t, `{"dashboard":{"name":"N","panels":[{"id":"p1","version":1,"queries":null}]}}`)
	result, err := ps.patch(t, `[{"op":"update_panel","panel_id":"p1","fields":{"queries":null}}]`)
	if err != nil {
		t.Fatal(err)
	}
	p1 := ps.sentDashboard(t)["panels"].([]any)[0].(map[string]any)
	if _, ok := p1["queries"]; ok {
		t.Fatalf("queries not removed: %v", p1)
	}
	if text := utils.GetTextContent(t, result); !strings.Contains(text, `"fields":{"queries":{"before":null}}`) {
		t.Fatalf("diff omits the deletion: %s", text)
	}
}

func TestPatchDashboard_RemovePanel(t *testing.T) {
	ps := newPatchServer(t)
	result, err := ps.patch(t, `[{"op":"remove_panel","panel_id":"p1"}]`)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := panelIDs(ps.sentDashboard(t)), []string{"p2", "s1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("panels %v want %v", got, want)
	}
	if text := utils.GetTextContent(t, result); !strings.Contains(text, `"op":"remove_panel","panel_id":"p1","name":"Latency"`) {
		t.Fatalf("diff %s", text)
	}
}

func TestPatchDashboard_MovePanel(t *testing.T) {
	ps := newPatchServer(t)
	result, err := ps.patch(t, `[{"op":"move_panel","panel_id":"s1","index":0}]`)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := panelIDs(ps.sentDashboard(t)), []string{"s1", "p1", "p2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("panels %v want %v", got, want)
	}
	if text := utils.GetTextContent(t, result); !strings.Contains(text, `"op":"move_panel","panel_id":"s1","from":2,"to":0`) {
		t.Fatalf("diff %s", text)
	}
}

func TestPatchDashboard_SetTitle(t *testing.T) {
	ps := newPatchServer(t)
	result, err := ps.patch(t, `[{"op":"set_title","title":"Gateway v2"}]`)
	if err != nil {
		t.Fatal(err)
	}
	doc := ps.sentDashboard(t)
	if doc["name"] != "Gateway v2" {
		t.Fatalf("name %v", doc["name"])
	}
	assertUntouchedExcept(t, doc, "name")
	if text := utils.GetTextContent(t, result); !strings.Contains(text, `"op":"set_title","before":"Gateway","after":"Gateway v2"`) {
		t.Fatalf("diff %s", text)
	}
}

func TestPatchDashboard_SetVariable(t *testing.T) {
	tests := []struct {
		name    string
		ops     string
		want    []any
		wantRaw string
	}{
		{
			name:    "replaces by target",
			ops:     `[{"op":"set_variable","variable":{"target":"env","type":"label","source":"deployment_environment"}}]`,
			want:    []any{map[string]any{"target": "env", "type": "label", "source": "deployment_environment"}},
			wantRaw: `"op":"set_variable","target":"env","action":"updated"`,
		},
		{
			name: "appends new target",
			ops:  `[{"op":"set_variable","variable":{"target":"service","type":"label","source":"service"}}]`,
			want: []any{
				map[string]any{"target": "env", "type": "label", "source": "env"},
				map[string]any{"target": "service", "type": "label", "source": "service"},
			},
			wantRaw: `"op":"set_variable","target":"service","action":"added"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ps := newPatchServer(t)
			result, err := ps.patch(t, tt.ops)
			if err != nil {
				t.Fatal(err)
			}
			doc := ps.sentDashboard(t)
			if !reflect.DeepEqual(doc["variables"], tt.want) {
				t.Fatalf("variables %v want %v", doc["variables"], tt.want)
			}
			assertUntouchedExcept(t, doc, "variables")
			if text := utils.GetTextContent(t, result); !strings.Contains(text, tt.wantRaw) {
				t.Fatalf("diff %s", text)
			}
		})
	}
}

func TestPatchDashboard_PreservesMetadataAndSendsRegion(t *testing.T) {
	ps := newPatchServer(t)
	result, err := ps.patch(t, `[{"op":"set_title","title":"x"}]`)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(ps.puts[0].Metadata); got != `{"_category":"custom","_type":"metrics"}` {
		t.Fatalf("metadata %s", got)
	}
	if len(ps.gets) != 1 || ps.gets[0] != "region=us-east-1" {
		t.Fatalf("GET queries %v", ps.gets)
	}
	if ref, _ := result.Meta["reference_url"].(string); ref != "/v2/organizations/test-org/dashboards/dash-1" {
		t.Fatalf("reference_url %q", ref)
	}
}

func TestPatchDashboard_AppliesOpsInOrder(t *testing.T) {
	ps := newPatchServer(t)
	if _, err := ps.patch(t, `[{"op":"add_panel","panel":{"id":"p3"}},{"op":"move_panel","panel_id":"p3","index":0},{"op":"remove_panel","panel_id":"p1"}]`); err != nil {
		t.Fatal(err)
	}
	if got, want := panelIDs(ps.sentDashboard(t)), []string{"p3", "p2", "s1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("panels %v want %v", got, want)
	}
}

func TestPatchDashboard_SinglePanelDiffIsCompact(t *testing.T) {
	ps := newPatchServer(t)
	longExpr := strings.Repeat("sum by (service) (rate(http_requests_total[5m])) + ", 60)
	ops, _ := json.Marshal([]map[string]any{{
		"op": "update_panel", "panel_id": "p1",
		"fields": map[string]any{"queries": []any{map[string]any{"name": "A", "expr": longExpr}}},
	}})
	result, err := ps.patch(t, string(ops))
	if err != nil {
		t.Fatal(err)
	}
	if text := utils.GetTextContent(t, result); len(text) > maxPatchDiffChars {
		t.Fatalf("diff is %d chars, want <= %d: %s", len(text), maxPatchDiffChars, text)
	}
	if maxPatchDiffChars > 2000 {
		t.Fatalf("maxPatchDiffChars = %d, issue caps a single-panel diff at 2000", maxPatchDiffChars)
	}
}

func TestPatchDashboard_RemovePanelDiffBoundsLongName(t *testing.T) {
	ps := newPatchServer(t)
	ops := `[{"op":"update_panel","panel_id":"p1","fields":{"name":"` + strings.Repeat("é", 1250) +
		`"}},{"op":"remove_panel","panel_id":"p1"}]`
	result, err := ps.patch(t, ops)
	if err != nil {
		t.Fatal(err)
	}
	text := utils.GetTextContent(t, result)
	if len(text) > maxPatchDiffChars {
		t.Fatalf("diff is %d chars, want <= %d: %s", len(text), maxPatchDiffChars, text)
	}
	if !utf8.ValidString(text) || !strings.Contains(text, `"name":"éé`) || !strings.Contains(text, `é…"`) {
		t.Fatalf("removed panel name not truncated on a rune boundary: %s", text)
	}
}

func TestPatchDashboard_InvalidOpNamesIndexAndSkipsWrite(t *testing.T) {
	tests := []struct {
		name    string
		ops     string
		wantErr string
		wantGet bool
	}{
		{name: "no ops", ops: `[]`, wantErr: "ops must contain at least one op"},
		{name: "unknown op", ops: `[{"op":"set_title","title":"x"},{"op":"rename"}]`, wantErr: `ops[1]: unknown op "rename"`},
		{name: "add_panel without panel", ops: `[{"op":"add_panel"}]`, wantErr: "ops[0] (add_panel): panel is required"},
		{name: "update_panel without fields", ops: `[{"op":"update_panel","panel_id":"p1"}]`, wantErr: "ops[0] (update_panel): fields is required"},
		{name: "update_panel changes id", ops: `[{"op":"update_panel","panel_id":"p1","fields":{"id":"p9"}}]`, wantErr: "ops[0] (update_panel): fields must not change id"},
		{name: "remove_panel without panel_id", ops: `[{"op":"remove_panel"}]`, wantErr: "ops[0] (remove_panel): panel_id is required"},
		{name: "move_panel without index", ops: `[{"op":"move_panel","panel_id":"p1"}]`, wantErr: "ops[0] (move_panel): index is required"},
		{name: "set_title empty", ops: `[{"op":"set_title","title":" "}]`, wantErr: "ops[0] (set_title): title is required"},
		{name: "set_variable without target", ops: `[{"op":"set_variable","variable":{"type":"label"}}]`, wantErr: "ops[0] (set_variable): variable.target is required"},
		{name: "missing panel", ops: `[{"op":"set_title","title":"x"},{"op":"remove_panel","panel_id":"nope"}]`, wantErr: `ops[1] (remove_panel): no panel with id "nope"; get_dashboard lists panel ids`, wantGet: true},
		{name: "duplicate panel id", ops: `[{"op":"add_panel","panel":{"id":"p1"}}]`, wantErr: `ops[0] (add_panel): a panel with id "p1" already exists`, wantGet: true},
		{name: "index out of range", ops: `[{"op":"move_panel","panel_id":"p1","index":3}]`, wantErr: "ops[0] (move_panel): index 3 out of range 0..2", wantGet: true},
		{name: "negative index", ops: `[{"op":"add_panel","panel":{"id":"p3"},"index":-1}]`, wantErr: "ops[0] (add_panel): index -1 out of range 0..3", wantGet: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ps := newPatchServer(t)
			_, err := ps.patch(t, tt.ops)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
			}
			if len(ps.puts) != 0 {
				t.Fatalf("PUT sent despite invalid op")
			}
			if got := len(ps.gets) > 0; got != tt.wantGet {
				t.Fatalf("GET sent = %v, want %v", got, tt.wantGet)
			}
		})
	}
}

func TestPatchDashboard_RequiresID(t *testing.T) {
	ps := newPatchServer(t)
	_, _, err := NewPatchDashboardHandler(ps.srv.Client(), testDashboardConfig(ps.srv.URL))(context.Background(), &mcp.CallToolRequest{}, PatchDashboardArgs{
		Ops: []PatchDashboardOp{{Op: "set_title", Title: "x"}},
	})
	if err == nil || !strings.Contains(err.Error(), "id is required") {
		t.Fatalf("err = %v", err)
	}
	if len(ps.gets)+len(ps.puts) != 0 {
		t.Fatal("API called for missing id")
	}
}

func TestPatchDashboard_NotFound(t *testing.T) {
	ps := newPatchServer(t)
	_, _, err := NewPatchDashboardHandler(ps.srv.Client(), testDashboardConfig(ps.srv.URL))(context.Background(), &mcp.CallToolRequest{}, PatchDashboardArgs{
		ID: "other", Ops: []PatchDashboardOp{{Op: "set_title", Title: "x"}},
	})
	if err == nil || !strings.Contains(err.Error(), "dashboard not found") {
		t.Fatalf("err = %v", err)
	}
}

func TestPatchDashboard_ForbiddenWriteIsReadonly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		_, _ = io.WriteString(w, patchFixture)
	}))
	defer srv.Close()
	_, _, err := NewPatchDashboardHandler(srv.Client(), testDashboardConfig(srv.URL))(context.Background(), &mcp.CallToolRequest{}, PatchDashboardArgs{
		ID: "dash-1", Ops: []PatchDashboardOp{{Op: "set_title", Title: "x"}},
	})
	if err == nil || !strings.Contains(err.Error(), "dashboard is readonly") {
		t.Fatalf("err = %v", err)
	}
}
