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

func registerPulseTools(reg func(error), server *last9mcp.Last9MCPServer, config models.Config, client *http.Client) {
	registerPulseSubscriptions(reg, server, config, client)
	registerPulseReports(reg, server, config, client)
	registerPulseDisposition(reg, server, config, client)
}

func registerPulseSubscriptions(reg func(error), server *last9mcp.Last9MCPServer, config models.Config, client *http.Client) {
	read := config.AllowedTools
	manage := toolsets.ManageOnly(config.AllowedTools)
	reg(registerIfAllowed(server, read, pulseReadTool("list_pulse_subscriptions", prompts.PulseSubscriptionsDescription), pulse.NewListSubscriptionsHandler(client, config)))
	reg(registerIfAllowed(server, read, pulseReadTool("get_pulse_subscription", prompts.GetPulseSubscriptionDescription), pulse.NewGetSubscriptionHandler(client, config)))
	reg(registerIfAllowed(server, manage, pulseWriteTool("create_pulse_subscription", prompts.CreatePulseSubscriptionDescription), pulse.NewCreateSubscriptionHandler(client, config)))
	reg(registerIfAllowed(server, manage, pulseWriteTool("update_pulse_subscription", prompts.UpdatePulseSubscriptionDescription), pulse.NewUpdateSubscriptionHandler(client, config)))
	reg(registerIfAllowed(server, manage, pulseWriteTool("enable_pulse_subscription", prompts.EnablePulseSubscriptionDescription), pulse.NewEnableSubscriptionHandler(client, config)))
	reg(registerIfAllowed(server, manage, pulseWriteTool("disable_pulse_subscription", prompts.DisablePulseSubscriptionDescription), pulse.NewDisableSubscriptionHandler(client, config)))
}

func registerPulseReports(reg func(error), server *last9mcp.Last9MCPServer, config models.Config, client *http.Client) {
	allowed := config.AllowedTools
	reg(registerIfAllowed(server, allowed, pulseReadTool("list_pulse_runs", prompts.PulseReportsDescription), pulse.NewListRunsHandler(client, config)))
	reg(registerIfAllowed(server, allowed, pulseReadTool("get_pulse_run", prompts.GetPulseRunDescription), pulse.NewGetRunHandler(client, config)))
	reg(registerIfAllowed(server, allowed, pulseReadTool("get_pulse_report", prompts.GetPulseReportDescription), pulse.NewGetReportHandler(client, config)))
	reg(registerIfAllowed(server, allowed, pulseReadTool("list_pulse_findings", prompts.ListPulseFindingsDescription), pulse.NewListFindingsHandler(client, config)))
	reg(registerIfAllowed(server, allowed, pulseReadTool("get_pulse_finding", prompts.GetPulseFindingDescription), pulse.NewGetFindingHandler(client, config)))
	reg(registerIfAllowed(server, allowed, pulseReadTool("list_pulse_evidence", prompts.ListPulseEvidenceDescription), pulse.NewListEvidenceHandler(client, config)))
}

func registerPulseDisposition(reg func(error), server *last9mcp.Last9MCPServer, config models.Config, client *http.Client) {
	tool := pulseWriteTool("write_pulse_disposition", prompts.PulseDispositionsDescription)
	reg(registerIfAllowed(server, toolsets.ManageOnly(config.AllowedTools), tool, pulse.NewWriteDispositionHandler(client, config)))
}

func pulseReadTool(name string, description string) *mcp.Tool {
	return &mcp.Tool{Name: name, Description: description, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}
}

func pulseWriteTool(name string, description string) *mcp.Tool {
	nonDestructive := false
	return &mcp.Tool{Name: name, Description: description, Annotations: &mcp.ToolAnnotations{DestructiveHint: &nonDestructive}}
}
