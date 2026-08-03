package workflows

import (
	"context"

	"last9-mcp/internal/prompts"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	investigateLatencySpikeName        = "investigate-latency-spike"
	investigateLatencySpikeTitle       = "Investigate Latency Spike"
	investigateLatencySpikeDescription = "Trace a service latency spike from performance metrics through deviations, traces, and downstream dependencies."
)

func investigateLatencySpikeHandler(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	var args map[string]string
	if req.Params != nil {
		args = req.Params.Arguments
	}
	return renderWorkflow(investigateLatencySpikeName, investigateLatencySpikeDescription,
		prompts.InvestigateLatencySpikeWorkflow, args, []string{"service", "time"})
}

var InvestigateLatencySpike = Workflow{
	Name:        investigateLatencySpikeName,
	Title:       investigateLatencySpikeTitle,
	Description: investigateLatencySpikeDescription,
	Arguments: []*mcp.PromptArgument{
		{Name: "service", Description: "Service to investigate", Required: true},
		{Name: "time", Description: "Time window to investigate, e.g. 1h or an absolute range; mapped to each tool's own time params", Required: true},
		{Name: "env", Description: "Deployment environment; discovered via get_service_environments if omitted", Required: false},
	},
	Handler: investigateLatencySpikeHandler,
}
