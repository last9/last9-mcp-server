package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/models"
	"last9-mcp/internal/toolsets"
	"last9-mcp/internal/workflows"

	last9mcp "github.com/last9/mcp-go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// dumpTools registers every tool exactly as the serving path does and writes
// the tools/list result ({"tools": [...]}, sorted by name) to w, by
// round-tripping a real tools/list over in-memory transports. Tool objects
// are emitted with the SDK's own wire marshaling, so the output matches what
// clients receive — including inputSchema, and annotations/outputSchema when
// present. The shape is compatible with .mcpc.json contract-snapshot tooling
// (e.g. mcpdiff).
//
// No credentials or network access required: registration never calls the
// API. Descriptions do not embed org attribute catalogs. External consumers
// (eval harness, docs generation) should treat this as the canonical source
// instead of maintaining parallel description files.
func dumpTools(w io.Writer, allowed toolsets.Set) error {
	cfg := models.Config{}
	// Purely defensive: registration and tools/list never dereference the
	// token manager (only tools/call handlers do), but set it so a future
	// handler constructor that touches it can't nil-panic on this path.
	cfg.TokenManager = &auth.TokenManager{}
	cfg.AllowedTools = allowed

	server, err := last9mcp.NewServerWithOptions("last9-mcp", Version, last9mcp.WithSkipProviderInit())
	if err != nil {
		return fmt.Errorf("failed to create server: %w", err)
	}

	registerReferenceResources(server)
	workflows.Register(server)

	if err := registerAllTools(server, cfg); err != nil {
		return fmt.Errorf("failed to register tools: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), dumpTimeout)
	defer cancel()
	session, closeSessions, err := connectInMemory(ctx, server, "dump-tools")
	if err != nil {
		return err
	}
	defer closeSessions()

	var tools []*mcp.Tool
	for tool, err := range session.Tools(ctx, nil) {
		if err != nil {
			return fmt.Errorf("failed to list tools: %w", err)
		}
		tools = append(tools, tool)
	}
	sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(map[string]any{"tools": tools})
}

// dumpPrompts writes the prompts/list result ({"prompts": [...]}, sorted by
// name) to w by round-tripping over in-memory transports — same pattern as
// dumpTools. No credentials needed; prompts aren't toolset-gated (no --toolsets).
func dumpPrompts(w io.Writer) error {
	server, err := last9mcp.NewServerWithOptions("last9-mcp", Version, last9mcp.WithSkipProviderInit())
	if err != nil {
		return fmt.Errorf("failed to create server: %w", err)
	}
	registerReferenceResources(server)
	workflows.Register(server)

	ctx, cancel := context.WithTimeout(context.Background(), dumpTimeout)
	defer cancel()
	session, closeSessions, err := connectInMemory(ctx, server, "dump-prompts")
	if err != nil {
		return err
	}
	defer closeSessions()

	var promptList []*mcp.Prompt
	for prompt, err := range session.Prompts(ctx, nil) {
		if err != nil {
			return fmt.Errorf("failed to list prompts: %w", err)
		}
		promptList = append(promptList, prompt)
	}
	sort.Slice(promptList, func(i, j int) bool { return promptList[i].Name < promptList[j].Name })

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(map[string]any{"prompts": promptList})
}

// The round-trips are over in-memory transports and won't hang in practice,
// but a timeout makes a wedged dump fail loudly in CI rather than hanging the
// gate.
const dumpTimeout = 30 * time.Second

// connectInMemory connects a client named clientName to server over in-memory
// transports. The returned func closes both sessions.
func connectInMemory(ctx context.Context, server *last9mcp.Last9MCPServer, clientName string) (*mcp.ClientSession, func(), error) {
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Server.Connect(ctx, serverTransport, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to connect server: %w", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: clientName, Version: Version}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		serverSession.Close()
		return nil, nil, fmt.Errorf("failed to connect client: %w", err)
	}
	return session, func() {
		session.Close()
		serverSession.Close()
	}, nil
}
