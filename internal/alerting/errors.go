package alerting

import (
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// toolErrorResult builds a user-facing MCP tool error (IsError=true) carrying msg as text.
// Use it for input-validation failures so the model receives a structured tool error
// rather than a transport-level protocol error.
func toolErrorResult(msg string) *mcp.CallToolResult {
	return utils.ToolErrorResult(msg)
}
