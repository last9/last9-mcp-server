package workflows

import (
	"context"

	"last9-mcp/internal/prompts"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	authorAlertName        = "author-alert"
	authorAlertTitle       = "Author Alert"
	authorAlertDescription = "Author a draft alert: discover/create KPIs, ground thresholds, validate, create disabled, promote only after confirmation."
)

var authorAlertArgs = []*mcp.PromptArgument{
	{Name: "goal", Description: "What should fire and why (e.g. p99 latency above SLO for checkout)", Required: true},
	{Name: "entity_id", Description: "Alert group UUID if already known; otherwise discover with get_alert_groups", Required: false},
	{Name: "signal", Description: "Optional signal hint: latency, error_rate, saturation, or a dashboard panel PromQL", Required: false},
}

func authorAlertHandler(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	var args map[string]string
	if req.Params != nil {
		args = req.Params.Arguments
	}
	return renderWorkflow(authorAlertName, authorAlertDescription,
		prompts.AuthorAlertWorkflow, args, authorAlertArgs)
}

var AuthorAlert = Workflow{
	Name:        authorAlertName,
	Title:       authorAlertTitle,
	Description: authorAlertDescription,
	Arguments:   authorAlertArgs,
	Handler:     authorAlertHandler,
}
