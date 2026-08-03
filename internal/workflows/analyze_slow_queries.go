package workflows

import (
	"context"

	"last9-mcp/internal/prompts"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	analyzeSlowQueriesName        = "analyze-slow-queries"
	analyzeSlowQueriesTitle       = "Analyze Slow Queries"
	analyzeSlowQueriesDescription = "Rank slow database queries and separate query-shape problems from server-resource pressure."
)

var analyzeSlowQueriesArgs = []*mcp.PromptArgument{
	{Name: "time", Description: "Time window to investigate, e.g. 1h or an absolute range; mapped to each tool's own time params", Required: true},
	{Name: "db_system", Description: "Database system filter (e.g. postgresql, mysql, mongodb, redis); surveys all systems if omitted", Required: false},
	{Name: "host", Description: "Database host filter (net_peer_name)", Required: false},
}

func analyzeSlowQueriesHandler(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	var args map[string]string
	if req.Params != nil {
		args = req.Params.Arguments
	}
	return renderWorkflow(analyzeSlowQueriesName, analyzeSlowQueriesDescription,
		prompts.AnalyzeSlowQueriesWorkflow, args, analyzeSlowQueriesArgs)
}

var AnalyzeSlowQueries = Workflow{
	Name:        analyzeSlowQueriesName,
	Title:       analyzeSlowQueriesTitle,
	Description: analyzeSlowQueriesDescription,
	Arguments:   analyzeSlowQueriesArgs,
	Handler:     analyzeSlowQueriesHandler,
}
