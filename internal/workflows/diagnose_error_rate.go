package workflows

import (
	"context"

	"last9-mcp/internal/prompts"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	diagnoseErrorRateName        = "diagnose-error-rate"
	diagnoseErrorRateTitle       = "Diagnose Error Rate"
	diagnoseErrorRateDescription = "Diagnose an elevated service error rate across performance metrics, exceptions, traces, aggregate logs, and dependencies."
)

var diagnoseErrorRateArgs = []*mcp.PromptArgument{
	{Name: "service", Description: "Service to investigate", Required: true},
	{Name: "time", Description: "Time window to investigate, e.g. 1h or an absolute range; mapped to each tool's own time params", Required: true},
	{Name: "env", Description: "Deployment environment; discovered via get_service_environments if omitted", Required: false},
}

func diagnoseErrorRateHandler(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	var args map[string]string
	if req.Params != nil {
		args = req.Params.Arguments
	}
	return renderWorkflow(diagnoseErrorRateName, diagnoseErrorRateDescription,
		prompts.DiagnoseErrorRateWorkflow, args, diagnoseErrorRateArgs)
}

var DiagnoseErrorRate = Workflow{
	Name:        diagnoseErrorRateName,
	Title:       diagnoseErrorRateTitle,
	Description: diagnoseErrorRateDescription,
	Arguments:   diagnoseErrorRateArgs,
	Handler:     diagnoseErrorRateHandler,
}
