package workflows

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// renderWorkflow validates that every argument marked Required in declared is
// present and non-blank, then executes the embedded text/template body against
// the string args and returns a single user message. Templates read args as
// {{.service}} and gate optional args with {{if .env}}...{{end}}; missing keys
// render as empty.
func renderWorkflow(name, description, tmpl string, args map[string]string, declared []*mcp.PromptArgument) (*mcp.GetPromptResult, error) {
	for _, a := range declared {
		if a.Required && strings.TrimSpace(args[a.Name]) == "" {
			return nil, fmt.Errorf("required argument %q is missing", a.Name)
		}
	}
	t, err := template.New(name).Option("missingkey=zero").Parse(tmpl)
	if err != nil {
		return nil, fmt.Errorf("parse %s template: %w", name, err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, args); err != nil {
		return nil, fmt.Errorf("render %s template: %w", name, err)
	}
	return &mcp.GetPromptResult{
		Description: description,
		Messages: []*mcp.PromptMessage{
			{Role: "user", Content: &mcp.TextContent{Text: buf.String()}},
		},
	}, nil
}
