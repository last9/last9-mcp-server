package workflows

import (
	"context"

	"last9-mcp/internal/prompts"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	scopedLogAttributeDiscoveryName        = "scoped-log-attribute-discovery"
	scopedLogAttributeDiscoveryTitle       = "Scoped Log Attribute Discovery"
	scopedLogAttributeDiscoveryDescription = "Discover service-scoped log fields before building aggregate log filters."
)

func scopedLogAttributeDiscoveryHandler(_ context.Context, _ *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	return &mcp.GetPromptResult{
		Description: scopedLogAttributeDiscoveryDescription,
		Messages: []*mcp.PromptMessage{
			{Role: "user", Content: &mcp.TextContent{Text: prompts.ScopedLogAttributeDiscoveryWorkflow}},
		},
	}, nil
}

var ScopedLogAttributeDiscovery = Workflow{
	Name:        scopedLogAttributeDiscoveryName,
	Title:       scopedLogAttributeDiscoveryTitle,
	Description: scopedLogAttributeDiscoveryDescription,
	Arguments:   nil,
	Handler:     scopedLogAttributeDiscoveryHandler,
}
