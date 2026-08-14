package workflows

import (
	"context"

	"last9-mcp/internal/prompts"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	exceptionRootCauseInvestigationName        = "exception-root-cause-investigation"
	exceptionRootCauseInvestigationTitle       = "Exception Root Cause Investigation"
	exceptionRootCauseInvestigationDescription = "Continue exception investigations into aggregate logs to find root-cause signals."
)

func exceptionRootCauseInvestigationHandler(_ context.Context, _ *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	return &mcp.GetPromptResult{
		Description: exceptionRootCauseInvestigationDescription,
		Messages: []*mcp.PromptMessage{
			{Role: "user", Content: &mcp.TextContent{Text: prompts.ExceptionRootCauseInvestigationWorkflow}},
		},
	}, nil
}

var ExceptionRootCauseInvestigation = Workflow{
	Name:        exceptionRootCauseInvestigationName,
	Title:       exceptionRootCauseInvestigationTitle,
	Description: exceptionRootCauseInvestigationDescription,
	Arguments:   nil,
	Handler:     exceptionRootCauseInvestigationHandler,
}
