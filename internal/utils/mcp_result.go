package utils

import "github.com/modelcontextprotocol/go-sdk/mcp"

// ToolErrorResult builds a user-facing MCP tool error (IsError=true) carrying msg as text.
// Use it for recoverable execution failures so the model receives a structured tool error
// rather than a transport-level protocol error.
func ToolErrorResult(msg string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: msg}},
		IsError: true,
	}
}
