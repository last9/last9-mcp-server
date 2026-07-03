package main

import (
	"context"
	"strings"
	"testing"

	"last9-mcp/internal/attributes"
	"last9-mcp/internal/auth"

	last9mcp "github.com/last9/mcp-go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// newInMemoryClientSession builds the full last9 MCP server (all tools
// registered) and connects an in-memory client to it, so tool calls exercise
// the SDK's real pre-handler schema-validation path.
func newInMemoryClientSession(t *testing.T) *mcp.ClientSession {
	t.Helper()

	cfg := testToolRegistrationConfig()
	server, err := last9mcp.NewServerWithOptions("last9-mcp-test", "test", last9mcp.WithSkipProviderInit())
	if err != nil {
		t.Fatalf("NewServerWithOptions error = %v", err)
	}

	attrCache := attributes.NewAttributeCache(auth.GetHTTPClient(), cfg)
	if err := registerAllTools(server, cfg, attrCache); err != nil {
		t.Fatalf("registerAllTools error = %v", err)
	}

	serverTransport, clientTransport := mcp.NewInMemoryTransports()

	ctx := context.Background()
	if _, err := server.Server.Connect(ctx, serverTransport, nil); err != nil {
		t.Fatalf("server.Connect error = %v", err)
	}

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "test"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect error = %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	return session
}

// TestSchemaValidationSurfacesAsToolError is the ENG-1319 regression guard:
// per MCP spec (SEP-1303), input-validation failures must reach the model as a
// CallToolResult{IsError:true} — NOT as a JSON-RPC -32602 protocol error the
// client swallows. CallTool must therefore return (result, nil) with
// result.IsError == true, never (nil, err).
func TestSchemaValidationSurfacesAsToolError(t *testing.T) {
	session := newInMemoryClientSession(t)
	ctx := context.Background()

	cases := []struct {
		name      string
		tool      string
		arguments map[string]any
		wantText  string // substring the surfaced error content should contain
	}{
		{
			name:      "unknown parameter name",
			tool:      "prometheus_labels",
			arguments: map[string]any{"selector": "up"},
			wantText:  "selector",
		},
		{
			name:      "wrong value type",
			tool:      "prometheus_instant_query",
			arguments: map[string]any{"query": 123},
			wantText:  "query",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := session.CallTool(ctx, &mcp.CallToolParams{
				Name:      tc.tool,
				Arguments: tc.arguments,
			})
			if err != nil {
				t.Fatalf("CallTool returned a protocol error (wrong channel — model can't see it): %v", err)
			}
			if res == nil {
				t.Fatalf("CallTool returned nil result")
			}
			if !res.IsError {
				t.Fatalf("want IsError=true (model-visible validation error), got IsError=false; content=%+v", res.Content)
			}

			var sb strings.Builder
			for _, c := range res.Content {
				if tc, ok := c.(*mcp.TextContent); ok {
					sb.WriteString(tc.Text)
				}
			}
			if got := sb.String(); !strings.Contains(got, tc.wantText) {
				t.Fatalf("surfaced error content missing %q: %s", tc.wantText, got)
			}
		})
	}
}
