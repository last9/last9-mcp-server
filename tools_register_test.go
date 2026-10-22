package main

import (
	"context"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"last9-mcp/internal/toolsets"

	last9mcp "github.com/last9/mcp-go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func newRegisterTestServer(t *testing.T) *last9mcp.Last9MCPServer {
	t.Helper()
	server, err := last9mcp.NewServerWithOptions("last9-mcp-register-test", Version, last9mcp.WithSkipProviderInit())
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	return server
}

// registeredToolNames round-trips a real tools/list over in-memory transports
// to observe exactly which tools the server would serve.
func registeredToolNames(t *testing.T, server *last9mcp.Last9MCPServer) []string {
	t.Helper()
	ctx := context.Background()
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("connect server: %v", err)
	}
	defer serverSession.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "register-test", Version: Version}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("connect client: %v", err)
	}
	defer session.Close()
	var names []string
	for tool, err := range session.Tools(ctx, nil) {
		if err != nil {
			t.Fatalf("list tools: %v", err)
		}
		names = append(names, tool.Name)
	}
	return names
}

func noopHandler(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, any, error) {
	return nil, nil, nil
}

func TestRegisterIfAllowedSkipsExcludedTools(t *testing.T) {
	server := newRegisterTestServer(t)
	gate := newToolGate(toolsets.Filter{Tools: toolsets.Set{"included_tool": {}}})
	if err := registerIfAllowed(server, gate, &mcp.Tool{Name: "excluded_tool"}, noopHandler); err != nil {
		t.Fatalf("skipped registration returned error: %v", err)
	}
	if err := registerIfAllowed(server, gate, &mcp.Tool{Name: "included_tool"}, noopHandler); err != nil {
		t.Fatalf("allowed registration failed: %v", err)
	}
	if err := gate.registerAdmitted(); err != nil {
		t.Fatalf("register admitted tools: %v", err)
	}
	names := registeredToolNames(t, server)
	if !reflect.DeepEqual(names, []string{"included_tool"}) {
		t.Fatalf("served tools = %v, want [included_tool]", names)
	}
}

func TestRegisterWriteToolRequiresVerifyCall(t *testing.T) {
	server := newRegisterTestServer(t)
	err := registerWriteTool(server, newToolGate(toolsets.Filter{}), &mcp.Tool{Name: "create_thing"}, " ", noopHandler)
	if err == nil || !strings.Contains(err.Error(), "create_thing") {
		t.Fatalf("want an error naming create_thing, got %v", err)
	}
	if names := registeredToolNames(t, server); len(names) != 0 {
		t.Fatalf("write tool without a verify call was served: %v", names)
	}
}

func TestRegisterWriteToolRequiresVerifyToolInSameToolset(t *testing.T) {
	server := newRegisterTestServer(t)
	allowed := toolsets.Set{"create_thing": {}}
	err := registerWriteTool(server, newToolGate(toolsets.Filter{Tools: allowed}), &mcp.Tool{Name: "create_thing"}, "get_things (name)", noopHandler)
	if err == nil || !strings.Contains(err.Error(), "get_things") {
		t.Fatalf("want an error naming get_things, got %v", err)
	}
	if names := registeredToolNames(t, server); len(names) != 0 {
		t.Fatalf("write tool whose verify tool is filtered out was served: %v", names)
	}
}

// Every toolset that serves a write tool must also serve the read tool its unknown-outcome
// error tells the agent to call.
func TestEveryToolsetServesWriteToolsVerifyCalls(t *testing.T) {
	for _, name := range toolsets.ValidNames() {
		t.Run(name, func(t *testing.T) {
			allowed, err := toolsets.Parse(name)
			if err != nil {
				t.Fatalf("parse %q: %v", name, err)
			}
			cfg := writeToolTestConfig("http://unused.invalid")
			cfg.ToolFilter = toolsets.Filter{Tools: allowed}
			if _, err := registerTools(newRegisterTestServer(t), cfg, http.DefaultClient); err != nil {
				t.Fatalf("register tools: %v", err)
			}
		})
	}
}

func TestRegisterIfAllowedRecoversSchemaPanic(t *testing.T) {
	server := newRegisterTestServer(t)
	chanHandler := func(context.Context, *mcp.CallToolRequest, chan int) (*mcp.CallToolResult, any, error) {
		return nil, nil, nil
	}
	gate := newToolGate(toolsets.Filter{})
	if err := registerIfAllowed(server, gate, &mcp.Tool{Name: "bad_schema_tool"}, chanHandler); err != nil {
		t.Fatalf("queueing an admitted tool returned error: %v", err)
	}
	err := gate.registerAdmitted()
	if err == nil {
		t.Fatal("expected error from panicking tool registration, got nil")
	}
	if !strings.Contains(err.Error(), "bad_schema_tool") {
		t.Fatalf("error %q should name the failing tool", err)
	}
}
