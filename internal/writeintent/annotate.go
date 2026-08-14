package writeintent

import "github.com/modelcontextprotocol/go-sdk/mcp"

// Annotate appends RefineNudge as Content[1] when id is non-empty.
// It does not copy or rewrite Content[0] or Meta. Nil-safe.
func Annotate(result *mcp.CallToolResult, p Pair, id string) *mcp.CallToolResult {
	if result == nil {
		return nil
	}
	nudge := RefineNudge(p, id)
	if nudge == "" {
		return result
	}
	result.Content = append(result.Content, &mcp.TextContent{Text: nudge})
	return result
}
