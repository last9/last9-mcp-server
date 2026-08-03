package workflows

import (
	"context"

	"last9-mcp/internal/prompts"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	onCallRunbookName        = "on-call-runbook"
	onCallRunbookTitle       = "On-Call Runbook"
	onCallRunbookDescription = "Symptom-routed on-call triage: dispatches latency, errors, or database investigation, with fleet triage when no service is given."
)

func onCallRunbookHandler(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	var args map[string]string
	if req.Params != nil {
		args = req.Params.Arguments
	}
	return renderWorkflow(onCallRunbookName, onCallRunbookDescription,
		prompts.OnCallRunbookWorkflow, args, []string{"symptom", "time"})
}

var OnCallRunbook = Workflow{
	Name:        onCallRunbookName,
	Title:       onCallRunbookTitle,
	Description: onCallRunbookDescription,
	Arguments: []*mcp.PromptArgument{
		{Name: "symptom", Description: "One of: latency, errors, database, unknown", Required: true},
		{Name: "time", Description: "Time window to investigate, e.g. 1h or an absolute range; mapped to each tool's own time params", Required: true},
		{Name: "service", Description: "Service to triage; fleet-wide triage (get_alerts, get_service_summary) if omitted", Required: false},
		{Name: "env", Description: "Deployment environment", Required: false},
	},
	Handler: onCallRunbookHandler,
}
