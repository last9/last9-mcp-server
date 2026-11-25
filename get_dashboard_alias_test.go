package main

import (
	"encoding/json"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGetDashboardAcceptsPatchResultID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || !strings.HasSuffix(r.URL.Path, "/dashboards/example") {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected", 400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"dashboard":{"id":"example","name":"Example","panels":[]}}`)
	}))
	defer srv.Close()
	session := connectTestSession(t, writeToolTestConfig(srv.URL), srv.Client())
	patch := callRegisteredTool(t, session, &mcp.CallToolParams{Name: "patch_dashboard", Arguments: json.RawMessage(`{"id":"example","ops":[{"op":"set_title","title":"Example"}]}`)})
	if patch.IsError {
		t.Fatal(toolResultText(patch))
	}
	var diff map[string]json.RawMessage
	if err := json.Unmarshal([]byte(toolResultText(patch)), &diff); err != nil {
		t.Fatal(err)
	}
	lookup := func(args any) *mcp.CallToolResult {
		return callRegisteredTool(t, session, &mcp.CallToolParams{Name: "get_dashboard", Arguments: args})
	}
	canonical := lookup(map[string]string{"id": "example"})
	alias := lookup(map[string]json.RawMessage{"dashboard_id": diff["dashboard_id"]})
	if canonical.IsError || alias.IsError || toolResultText(canonical) != toolResultText(alias) {
		t.Fatalf("canonical %s, alias %s", toolResultText(canonical), toolResultText(alias))
	}
	for _, tc := range []struct{ args, want string }{
		{`{"dashboard_id":"example","id":"other"}`, "conflicting"},
		{`{"dashboard_id":null}`, "string"},
		{`{"dashboard_id":"example","extra":1}`, "extra"},
	} {
		r := lookup(json.RawMessage(tc.args))
		if !r.IsError || !strings.Contains(toolResultText(r), tc.want) {
			t.Errorf("%s: %s", tc.args, toolResultText(r))
		}
	}
}
