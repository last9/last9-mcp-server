package writeintent

import "fmt"

// RefineNudge is the second MCP text part after a successful create.
// Empty id returns "" — never invent an id.
func RefineNudge(p Pair, id string) string {
	if id == "" {
		return ""
	}
	return fmt.Sprintf(
		"To refine this %s, call %s with %s=%s. Do not call %s again.",
		p.Resource, p.UpdateTool, p.IDField, id, p.CreateTool,
	)
}
