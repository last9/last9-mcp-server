package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"time"

	"last9-mcp/internal/attributes"
	"last9-mcp/internal/auth"
	"last9-mcp/internal/models"

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
// API. Dynamic description enhancement degrades the same way it does on a
// cold start — the {{labels}} substitution is empty — which is the
// deterministic "default experience" snapshot (a warning is printed to
// stderr). External consumers (eval harness, docs generation) should treat
// this as the canonical source instead of maintaining parallel description
// files.
func dumpTools(w io.Writer) error {
	cfg := models.Config{}
	// Purely defensive: registration and tools/list never dereference the
	// token manager (only tools/call handlers do), but set it so a future
	// handler constructor that touches it can't nil-panic on this path.
	cfg.TokenManager = &auth.TokenManager{}

	server, err := last9mcp.NewServerWithOptions("last9-mcp", Version, last9mcp.WithSkipProviderInit())
	if err != nil {
		return fmt.Errorf("failed to create server: %w", err)
	}

	attrCache := attributes.NewAttributeCache(auth.GetHTTPClient(), cfg)
	if err := registerAllTools(server, cfg, attrCache); err != nil {
		return fmt.Errorf("failed to register tools: %w", err)
	}
	fmt.Fprintln(os.Stderr, "note: label cache is cold; {{labels}} placeholders substitute to empty (deterministic default snapshot)")

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

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(map[string]any{"tools": tools})
}
