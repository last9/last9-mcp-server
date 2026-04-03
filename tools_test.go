package main

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"last9-mcp/internal/attributes"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	last9mcp "github.com/last9/mcp-go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestRegisterAllTools_RegistersDidYouMeanAndHints(t *testing.T) {
	ctx := context.Background()
	server := newLast9TestServer(t)
	cfg := models.Config{}
	attrCache := attributes.NewAttributeCache(http.DefaultClient, cfg)

	if err := registerAllTools(server, cfg, attrCache); err != nil {
		t.Fatalf("registerAllTools failed: %v", err)
	}

	clientSession := connectTestClient(t, ctx, server)

	res, err := clientSession.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools failed: %v", err)
	}

	toolsByName := map[string]*mcp.Tool{}
	for _, tool := range res.Tools {
		toolsByName[tool.Name] = tool
	}

	didYouMean := toolsByName["did_you_mean"]
	if didYouMean == nil {
		t.Fatal("did_you_mean tool was not registered")
	}
	for _, snippet := range []string{
		"Suggests correct entity names",
		"Returns up to 3 closest matches",
		"query:",
		"type:",
	} {
		if !strings.Contains(didYouMean.Description, snippet) {
			t.Fatalf("did_you_mean description missing %q", snippet)
		}
	}

	for _, toolName := range []string{
		"get_service_performance_details",
		"get_service_operations_summary",
		"get_service_dependency_graph",
		"get_database_slow_queries",
		"get_database_queries",
		"get_service_logs",
		"get_service_traces",
		"get_exceptions",
		"get_change_events",
	} {
		tool := toolsByName[toolName]
		if tool == nil {
			t.Fatalf("%s tool was not registered", toolName)
		}
		if !strings.Contains(tool.Description, "did_you_mean") {
			t.Fatalf("%s description does not mention did_you_mean", toolName)
		}
	}
}

func TestRegisterInstrumentedTool_CoercesStringNumbersBeforeValidation(t *testing.T) {
	type coercionArgs struct {
		LookbackMinutes int `json:"lookback_minutes,omitempty" jsonschema:"Number of minutes to look back from now (default: 60, minimum: 1)"`
	}

	ctx := context.Background()
	server := newLast9TestServer(t)

	err := last9mcp.RegisterInstrumentedTool(server, &mcp.Tool{
		Name:        "coerce_lookback_minutes",
		Description: "Echo the parsed lookback_minutes value",
	}, func(_ context.Context, _ *mcp.CallToolRequest, args coercionArgs) (*mcp.CallToolResult, any, error) {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: strconv.Itoa(args.LookbackMinutes)},
			},
		}, nil, nil
	})
	if err != nil {
		t.Fatalf("RegisterInstrumentedTool failed: %v", err)
	}

	clientSession := connectTestClient(t, ctx, server)

	result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name: "coerce_lookback_minutes",
		Arguments: map[string]any{
			"lookback_minutes": "60",
		},
	})
	if err != nil {
		t.Fatalf("CallTool failed: %v", err)
	}
	if result.IsError {
		t.Fatalf("CallTool returned tool error: %#v", result)
	}
	if got := utils.GetTextContent(t, result); got != "60" {
		t.Fatalf("expected coerced lookback_minutes to be 60, got %q", got)
	}
}

func newLast9TestServer(t *testing.T) *last9mcp.Last9MCPServer {
	t.Helper()

	server, err := last9mcp.NewServer("last9-mcp-test", "test")
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}
	return server
}

func connectTestClient(t *testing.T, ctx context.Context, server *last9mcp.Last9MCPServer) *mcp.ClientSession {
	t.Helper()

	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server connect failed: %v", err)
	}
	t.Cleanup(func() {
		_ = serverSession.Close()
	})

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v1.0.0"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client connect failed: %v", err)
	}
	t.Cleanup(func() {
		_ = clientSession.Close()
	})

	return clientSession
}
