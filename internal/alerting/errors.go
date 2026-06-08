package alerting

import "github.com/modelcontextprotocol/go-sdk/mcp"

// toolErrorResult builds a user-facing MCP tool error (IsError=true) carrying msg as text.
// Use it for input-validation failures so the model receives a structured tool error
// rather than a transport-level protocol error.
func toolErrorResult(msg string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: msg}},
		IsError: true,
	}
}
