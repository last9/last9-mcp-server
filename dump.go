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

// dumpTools registers every tool and the server instructions exactly as the
// serving path does and writes the tools/list result (sorted by name) plus the
// instructions initialize returns ({"tools": [...], "instructions": "..."}) to
// w, by round-tripping a real initialize and tools/list over in-memory
// transports. Tool objects are emitted with the SDK's own wire marshaling, so
// the output matches what clients receive — including inputSchema, and
// annotations/outputSchema when present. The shape is compatible with
// .mcpc.json contract-snapshot tooling (e.g. mcpdiff).
//
// No credentials or network access required: registration never calls the
// API. Descriptions do not embed org attribute catalogs. External consumers
// (eval harness, docs generation) should treat this as the canonical source
// instead of maintaining parallel description files.
func dumpTools(w io.Writer, filter toolsets.Filter) error {
	cfg := models.Config{}
	// Purely defensive: registration and tools/list never dereference the
	// token manager (only tools/call handlers do), but set it so a future
	// handler constructor that touches it can't nil-panic on this path.
	cfg.TokenManager = &auth.TokenManager{}
	cfg.ToolFilter = filter

	server, err := last9mcp.NewServerWithOptions("last9-mcp", Version, last9mcp.WithSkipProviderInit())
	if err != nil {
		return fmt.Errorf("failed to create server: %w", err)
	}

	registerReferenceResources(server)
	workflows.Register(server)

	if err := registerToolsAndInstructions(server, cfg); err != nil {
		return err
	}
	// The round-trip is over in-memory transports and won't hang in practice,
	// but a timeout makes a wedged tools/list fail loudly in CI rather than
	// hanging the gate.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Server.Connect(ctx, serverTransport, nil)
	if err != nil {
		return fmt.Errorf("failed to connect server: %w", err)
	}
	defer serverSession.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "dump-tools", Version: Version}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		return fmt.Errorf("failed to connect client: %w", err)
	}
	defer session.Close()

	var tools []*mcp.Tool
	for tool, err := range session.Tools(ctx, nil) {
		if err != nil {
			return fmt.Errorf("failed to list tools: %w", err)
		}
		tools = append(tools, tool)
	}
	sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })

	// A struct, not a map, keeps "tools" first so existing snapshot diffs only
	// gain trailing lines.
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(struct {
		Tools        []*mcp.Tool `json:"tools"`
		Instructions string      `json:"instructions"`
	}{tools, session.InitializeResult().Instructions})
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

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Server.Connect(ctx, serverTransport, nil)
	if err != nil {
		return fmt.Errorf("failed to connect server: %w", err)
	}
	defer serverSession.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "dump-prompts", Version: Version}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		return fmt.Errorf("failed to connect client: %w", err)
	}
	defer session.Close()

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
