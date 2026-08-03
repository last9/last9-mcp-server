package workflows

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"

	last9mcp "github.com/last9/mcp-go-sdk/mcp"
)

// Workflow is one MCP prompt: its advertised metadata plus the handler that
// renders it. Arguments is nil for parameter-less prompts.
type Workflow struct {
	Name        string
	Title       string
	Description string
	Arguments   []*mcp.PromptArgument
	Handler     mcp.PromptHandler
}

func registerWorkflowPrompt(server *last9mcp.Last9MCPServer, w Workflow) {
	server.Server.AddPrompt(&mcp.Prompt{
		Name:        w.Name,
		Title:       w.Title,
		Description: w.Description,
		Arguments:   w.Arguments,
	}, w.Handler)
}

// Register adds every workflow prompt to the MCP server. Add a new one by
// declaring a Workflow value and listing it here.
func Register(server *last9mcp.Last9MCPServer) {
	registerWorkflowPrompt(server, ScopedLogAttributeDiscovery)
	registerWorkflowPrompt(server, ExceptionRootCauseInvestigation)
	registerWorkflowPrompt(server, InvestigateLatencySpike)
	registerWorkflowPrompt(server, DiagnoseErrorRate)
	registerWorkflowPrompt(server, AnalyzeSlowQueries)
	registerWorkflowPrompt(server, OnCallRunbook)
}
