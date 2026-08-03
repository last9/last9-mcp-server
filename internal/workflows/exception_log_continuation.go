package workflows

import (
	"context"

	"last9-mcp/internal/prompts"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	exceptionLogContinuationName        = "exception-log-continuation"
	exceptionLogContinuationTitle       = "Exception Log Continuation"
	exceptionLogContinuationDescription = "Continue exception investigations into aggregate logs to find root-cause signals."
)

func exceptionLogContinuationHandler(_ context.Context, _ *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	return &mcp.GetPromptResult{
		Description: exceptionLogContinuationDescription,
		Messages: []*mcp.PromptMessage{
			{Role: "user", Content: &mcp.TextContent{Text: prompts.ExceptionLogContinuationWorkflow}},
		},
	}, nil
}

var ExceptionLogContinuation = Workflow{
	Name:        exceptionLogContinuationName,
	Title:       exceptionLogContinuationTitle,
	Description: exceptionLogContinuationDescription,
	Arguments:   nil,
	Handler:     exceptionLogContinuationHandler,
}
