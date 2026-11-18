package utils

import "github.com/modelcontextprotocol/go-sdk/mcp"

// GuidanceCauseMetaKey carries an error result's l9_guidance cause. The tool-call
// deadline keeps a GuidanceCauseTimeout result, whose next steps are specific to
// its tool, instead of replacing it with generic text.
const (
	GuidanceCauseMetaKey = "l9_guidance_cause"
	GuidanceCauseTimeout = "timeout"
)

func HasTimeoutGuidance(result *mcp.CallToolResult) bool {
	return result != nil && result.IsError && result.Meta[GuidanceCauseMetaKey] == GuidanceCauseTimeout
}

// ToolErrorResult builds a user-facing MCP tool error (IsError=true) carrying msg as text.
// Use it for recoverable execution failures so the model receives a structured tool error
// rather than a transport-level protocol error.
func ToolErrorResult(msg string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: msg}},
		IsError: true,
	}
}
