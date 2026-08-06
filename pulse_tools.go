package main

import (
	"net/http"

	"last9-mcp/internal/models"
	"last9-mcp/internal/prompts"
	"last9-mcp/internal/pulse"
	"last9-mcp/internal/toolsets"

	last9mcp "github.com/last9/mcp-go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func registerPulseTools(server *last9mcp.Last9MCPServer, config models.Config, client *http.Client) error {
	registrar := &pulseRegistrar{server: server}
	registerPulseSubscriptions(registrar, config, client)
	registerPulseReports(registrar, config, client)
	registerPulseDisposition(registrar, config, client)
	return registrar.err
}

type pulseRegistrar struct {
	server *last9mcp.Last9MCPServer
	err    error
}

func registerPulseHandler[In, Out any](registrar *pulseRegistrar, allowed toolsets.Set, tool *mcp.Tool, handler mcp.ToolHandlerFor[In, Out]) {
	if registrar.err != nil {
		return
	}
	registrar.err = registerIfAllowed(registrar.server, allowed, tool, handler)
}

func registerPulseSubscriptions(registrar *pulseRegistrar, config models.Config, client *http.Client) {
	read := config.AllowedTools
	manage := toolsets.ManageOnly(config.AllowedTools)
	registerPulseHandler(registrar, read, pulseReadTool("list_pulse_subscriptions", prompts.PulseSubscriptionsDescription), pulse.NewListSubscriptionsHandler(client, config))
	registerPulseHandler(registrar, read, pulseReadTool("get_pulse_subscription", prompts.PulseSubscriptionsDescription), pulse.NewGetSubscriptionHandler(client, config))
	registerPulseHandler(registrar, manage, pulseWriteTool("create_pulse_subscription", prompts.PulseSubscriptionsDescription), pulse.NewCreateSubscriptionHandler(client, config))
	registerPulseHandler(registrar, manage, pulseWriteTool("update_pulse_subscription", prompts.PulseSubscriptionsDescription), pulse.NewUpdateSubscriptionHandler(client, config))
	registerPulseHandler(registrar, manage, pulseWriteTool("enable_pulse_subscription", prompts.PulseSubscriptionsDescription), pulse.NewEnableSubscriptionHandler(client, config))
	registerPulseHandler(registrar, manage, pulseWriteTool("disable_pulse_subscription", prompts.PulseSubscriptionsDescription), pulse.NewDisableSubscriptionHandler(client, config))
}

func registerPulseReports(registrar *pulseRegistrar, config models.Config, client *http.Client) {
	allowed := config.AllowedTools
	registerPulseHandler(registrar, allowed, pulseReadTool("list_pulse_runs", prompts.PulseReportsDescription), pulse.NewListRunsHandler(client, config))
	registerPulseHandler(registrar, allowed, pulseReadTool("get_pulse_run", prompts.PulseReportsDescription), pulse.NewGetRunHandler(client, config))
	registerPulseHandler(registrar, allowed, pulseReadTool("get_pulse_report", prompts.PulseReportsDescription), pulse.NewGetReportHandler(client, config))
	registerPulseHandler(registrar, allowed, pulseReadTool("list_pulse_findings", prompts.PulseReportsDescription), pulse.NewListFindingsHandler(client, config))
	registerPulseHandler(registrar, allowed, pulseReadTool("get_pulse_finding", prompts.PulseReportsDescription), pulse.NewGetFindingHandler(client, config))
	registerPulseHandler(registrar, allowed, pulseReadTool("list_pulse_evidence", prompts.PulseReportsDescription), pulse.NewListEvidenceHandler(client, config))
}

func registerPulseDisposition(registrar *pulseRegistrar, config models.Config, client *http.Client) {
	tool := pulseWriteTool("write_pulse_disposition", prompts.PulseDispositionsDescription)
	registerPulseHandler(registrar, toolsets.ManageOnly(config.AllowedTools), tool, pulse.NewWriteDispositionHandler(client, config))
}

func pulseReadTool(name string, description string) *mcp.Tool {
	return &mcp.Tool{Name: name, Description: description, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}
}

func pulseWriteTool(name string, description string) *mcp.Tool {
	nonDestructive := false
	return &mcp.Tool{Name: name, Description: description, Annotations: &mcp.ToolAnnotations{DestructiveHint: &nonDestructive}}
}
