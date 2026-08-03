package workflows

import (
	"context"
	"strings"

	"last9-mcp/internal/prompts"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	onCallRunbookName        = "on-call-runbook"
	onCallRunbookTitle       = "On-Call Runbook"
	onCallRunbookDescription = "Symptom-routed on-call triage: dispatches latency, errors, or database investigation, with fleet triage when no service is given."
)

var onCallRunbookArgs = []*mcp.PromptArgument{
	{Name: "symptom", Description: "latency, errors, or database (synonyms fold in: slow→latency, db→database); anything else triages via get_alerts", Required: true},
	{Name: "time", Description: "Time window to investigate, e.g. 1h or an absolute range; mapped to each tool's own time params", Required: true},
	{Name: "service", Description: "Service to triage; fleet-wide triage (get_alerts, get_service_summary) if omitted", Required: false},
	{Name: "env", Description: "Deployment environment", Required: false},
}

// canonicalSymptoms folds case/plural/short variants onto the three values the
// template routes on. Unlisted values fall through to the "unknown" branch,
// never a wrong one.
var canonicalSymptoms = map[string]string{
	"latency":    "latency",
	"slow":       "latency",
	"slowness":   "latency",
	"errors":     "errors",
	"error":      "errors",
	"exception":  "errors",
	"exceptions": "errors",
	"database":   "database",
	"db":         "database",
	"sql":        "database",
}

func onCallRunbookHandler(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	var args map[string]string
	if req.Params != nil {
		args = req.Params.Arguments
	}
	// args != nil guards the write: a nil map read is fine, a nil map write panics.
	if args != nil {
		if canon, ok := canonicalSymptoms[strings.ToLower(strings.TrimSpace(args["symptom"]))]; ok {
			args["symptom"] = canon
		}
	}
	return renderWorkflow(onCallRunbookName, onCallRunbookDescription,
		prompts.OnCallRunbookWorkflow, args, onCallRunbookArgs)
}

var OnCallRunbook = Workflow{
	Name:        onCallRunbookName,
	Title:       onCallRunbookTitle,
	Description: onCallRunbookDescription,
	Arguments:   onCallRunbookArgs,
	Handler:     onCallRunbookHandler,
}
