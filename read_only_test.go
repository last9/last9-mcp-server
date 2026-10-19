package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"testing"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/models"
	"last9-mcp/internal/prompts"
	"last9-mcp/internal/toolsets"

	last9mcp "github.com/last9/mcp-go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type servedTool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Annotations *struct {
		ReadOnlyHint bool `json:"readOnlyHint"`
	} `json:"annotations"`
}

func dumpServedTools(t *testing.T, filter toolsets.Filter) []servedTool {
	t.Helper()
	var buf bytes.Buffer
	if err := dumpTools(&buf, filter); err != nil {
		t.Fatalf("dumpTools(%+v): %v", filter, err)
	}
	var out struct {
		Tools []servedTool `json:"tools"`
	}
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	return out.Tools
}

func servedNames(tools []servedTool) []string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	sort.Strings(names)
	return names
}

// readToolNames is the full served surface minus every writeTools entry.
func readToolNames(t *testing.T, filter toolsets.Filter) []string {
	t.Helper()
	var names []string
	for _, name := range servedNames(dumpServedTools(t, filter)) {
		if _, isWrite := writeTools[name]; !isWrite {
			names = append(names, name)
		}
	}
	return names
}

func withNames(names []string, extra ...string) []string {
	out := append(append([]string{}, names...), extra...)
	sort.Strings(out)
	return out
}

func TestDumpToolsReadOnlyServesOnlyReadTools(t *testing.T) {
	served := dumpServedTools(t, toolsets.Filter{ReadOnly: true})
	for _, tool := range served {
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Errorf("%s: served in read-only mode without readOnlyHint: true", tool.Name)
		}
	}
	if got, want := servedNames(served), readToolNames(t, toolsets.Filter{}); !reflect.DeepEqual(got, want) {
		t.Fatalf("read-only surface = %v\nwant all tools minus writeTools = %v", got, want)
	}
}

func TestDumpToolsReadOnlyAllowWriteTools(t *testing.T) {
	filter := toolsets.Filter{ReadOnly: true, AllowWrite: toolsets.Set{"create_alert": {}, "add_drop_rule": {}}}
	got := servedNames(dumpServedTools(t, filter))
	want := withNames(readToolNames(t, toolsets.Filter{}), "add_drop_rule", "create_alert")
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("served = %v\nwant read tools plus add_drop_rule, create_alert = %v", got, want)
	}
}

func TestDumpToolsReadOnlyWithToolsets(t *testing.T) {
	alerts, err := toolsets.Parse("alerts")
	if err != nil {
		t.Fatal(err)
	}
	// create_dashboard is a real write tool outside alerts: it is not served,
	// and not an error, because both filters must pass.
	filter := toolsets.Filter{Tools: alerts, ReadOnly: true, AllowWrite: toolsets.Set{"mute_alert_group": {}, "create_dashboard": {}}}
	got := servedNames(dumpServedTools(t, filter))
	want := withNames(readToolNames(t, toolsets.Filter{Tools: alerts}), "mute_alert_group")
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("served = %v\nwant alerts read tools plus mute_alert_group = %v", got, want)
	}
	if len(want) < 2 {
		t.Fatalf("alerts read surface unexpectedly empty: %v", want)
	}
}

// Routing sections may only name tools the same server serves, so a write
// tool admitted by --allow-write-tools must not route to a hidden sibling.
func TestDumpToolsReadOnlyRoutesNameOnlyServedTools(t *testing.T) {
	dashboards, err := toolsets.Parse("dashboards")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		filter toolsets.Filter
		routes []string // a route that must survive, proving the check is live
	}{
		{name: "create only", filter: toolsets.Filter{Tools: dashboards, ReadOnly: true, AllowWrite: toolsets.Set{"create_dashboard": {}}}, routes: []string{"validate_dashboard"}},
		{name: "create and update", filter: toolsets.Filter{Tools: dashboards, ReadOnly: true, AllowWrite: toolsets.Set{"create_dashboard": {}, "update_dashboard": {}}}, routes: []string{"update_dashboard"}},
		{name: "full read-only surface", filter: toolsets.Filter{ReadOnly: true}},
		{name: "full surface plus create_alert", filter: toolsets.Filter{ReadOnly: true, AllowWrite: toolsets.Set{"create_alert": {}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tools := dumpServedTools(t, tt.filter)
			served := toolsets.Set{}
			for _, tool := range tools {
				served[tool.Name] = struct{}{}
			}
			routed := toolsets.Set{}
			for _, tool := range tools {
				for _, section := range prompts.Sections(tool.Description) {
					if !prompts.IsRoutingSection(section.Name) {
						continue
					}
					for _, name := range prompts.RouteNames(section.Body) {
						routed[name] = struct{}{}
						if !served.Allows(name) {
							t.Errorf("%s routes to unserved %s in %q", tool.Name, name, section.Name)
						}
					}
				}
			}
			for _, name := range tt.routes {
				if !routed.Allows(name) {
					t.Errorf("no served description routes to %s; routes = %v", name, routed)
				}
			}
		})
	}
}

func TestDumpToolsRejectsBadAllowWriteNames(t *testing.T) {
	tests := []struct {
		name    string
		allow   string
		wantErr string
	}{
		{name: "unknown name", allow: "create_alrt", wantErr: `unknown tool "create_alrt"`},
		{name: "read tool name", allow: "get_alerts", wantErr: `"get_alerts" is read-only`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filter := toolsets.Filter{ReadOnly: true, AllowWrite: toolsets.Set{tt.allow: {}, "create_alert": {}}}
			err := dumpTools(&bytes.Buffer{}, filter)
			if err == nil {
				t.Fatalf("dumpTools with --allow-write-tools=%s succeeded, want a startup error", tt.allow)
			}
			if !strings.Contains(err.Error(), tt.wantErr) || !strings.Contains(err.Error(), "create_alert") {
				t.Fatalf("error %q should contain %q and list the valid write tools", err, tt.wantErr)
			}
		})
	}
}

func TestRegisterIfAllowedReadOnlyFailsClosedWithoutAnnotations(t *testing.T) {
	server := newRegisterTestServer(t)
	gate := newToolGate(toolsets.Filter{ReadOnly: true})
	tools := []*mcp.Tool{
		{Name: "unannotated_tool"},
		{Name: "write_tool", Annotations: writeTool("Write", false, false)},
		{Name: "read_tool", Annotations: readOnlyTool("Read")},
	}
	for _, tool := range tools {
		if err := registerIfAllowed(server, gate, tool, noopHandler); err != nil {
			t.Fatalf("register %s: %v", tool.Name, err)
		}
	}
	if names := registeredToolNames(t, server); !reflect.DeepEqual(names, []string{"read_tool"}) {
		t.Fatalf("served tools = %v, want [read_tool]", names)
	}
}

func TestHTTPModeHonorsReadOnly(t *testing.T) {
	server, err := last9mcp.NewServerWithOptions("last9-mcp", Version, last9mcp.WithSkipProviderInit())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Shutdown(context.Background()) })
	cfg := models.Config{TokenManager: &auth.TokenManager{}, ToolFilter: toolsets.Filter{ReadOnly: true}}
	if _, err := registerAllTools(server, cfg); err != nil {
		t.Fatalf("registerAllTools: %v", err)
	}
	ts := httptest.NewServer(server.NewStreamableHTTPHandler(&mcp.StreamableHTTPOptions{Stateless: true}))
	defer ts.Close()

	ctx := context.Background()
	client := mcp.NewClient(&mcp.Implementation{Name: "read-only-http-test", Version: Version}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: ts.URL}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer session.Close()
	var listed []string
	for tool, err := range session.Tools(ctx, nil) {
		if err != nil {
			t.Fatalf("list tools: %v", err)
		}
		listed = append(listed, tool.Name)
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Errorf("%s: listed over HTTP in read-only mode without readOnlyHint: true", tool.Name)
		}
	}
	sort.Strings(listed)
	if want := readToolNames(t, toolsets.Filter{}); !reflect.DeepEqual(listed, want) {
		t.Fatalf("HTTP tools/list = %v\nwant %v", listed, want)
	}
}
