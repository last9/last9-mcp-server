package workflows

import (
	"context"

	"last9-mcp/internal/prompts"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	authorAlertName        = "author-alert"
	authorAlertTitle       = "Author Alert"
	authorAlertDescription = "Write a Last9 alert rule end to end: find the alert group and its KPIs, ground the threshold in real data, validate the expression, create a disabled draft, read it back, and promote it only after the user confirms. Also covers alerting on a dashboard panel."
)

var authorAlertArgs = []*mcp.PromptArgument{
	{Name: "goal", Description: "What the alert should detect, e.g. checkout p99 latency above 500ms", Required: true},
	{Name: "alert_group", Description: "Alert group name to put the rule in; discovered with get_alert_groups if omitted", Required: false},
	{Name: "dashboard_id", Description: "Custom dashboard UUID, to alert on one of its panels", Required: false},
	{Name: "panel", Description: "Panel name on that dashboard", Required: false},
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
