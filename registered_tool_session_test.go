package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"last9-mcp/internal/toolsets"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type registeredToolFixture struct {
	tool       *mcp.Tool
	timeout    time.Duration
	verify     string
	middleware mcp.Middleware
}

func connectToolFixture[In, Out any](t *testing.T, fixture registeredToolFixture, handler mcp.ToolHandlerFor[In, Out]) *mcp.ClientSession {
	t.Helper()
	server := newRegisterTestServer(t)
	gate := newToolGate(toolsets.Filter{})
	gate.callTimeout = fixture.timeout
	var err error
	if fixture.verify != "" {
		err = registerWriteTool(server, gate, fixture.tool, fixture.verify, handler)
	} else {
		err = registerIfAllowed(server, gate, fixture.tool, handler)
	}
	if err != nil {
		t.Fatalf("register %s: %v", fixture.tool.Name, err)
	}
	if err := gate.registerAdmitted(); err != nil {
		t.Fatalf("register admitted: %v", err)
	}
	if fixture.middleware != nil {
		server.Server.AddReceivingMiddleware(fixture.middleware)
	}
	return connectRegisteredServer(t, server)
}

func callRegisteredTool(t *testing.T, session *mcp.ClientSession, params *mcp.CallToolParams) *mcp.CallToolResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := session.CallTool(ctx, params)
	if err != nil {
		t.Fatalf("call %s: %v", params.Name, err)
	}
	return result
}

type toolErrorExpectation struct {
	contains []string
	absent   []string
	system   bool
}

func assertToolError(t *testing.T, result *mcp.CallToolResult, want toolErrorExpectation) {
	t.Helper()
	if result == nil || !result.IsError {
		t.Fatalf("want isError result, got %+v", result)
	}
	text := toolResultText(result)
	for _, fragment := range want.contains {
		if !strings.Contains(text, fragment) {
			t.Errorf("missing %q in tool error %q", fragment, text)
		}
	}
	for _, fragment := range want.absent {
		if strings.Contains(text, fragment) {
			t.Errorf("unexpected %q in tool error %q", fragment, text)
		}
	}
	got, tagged := result.Meta[utils.ErrorTypeMetaKey]
	if want.system && got != utils.SystemErrorType || !want.system && tagged {
		t.Errorf("want system_error tag=%v, got meta=%v", want.system, result.Meta)
	}
	if _, ok := result.Meta["io.modelcontextprotocol/serverInfo"]; !ok {
		t.Errorf("missing server metadata: %v", result.Meta)
	}
}
