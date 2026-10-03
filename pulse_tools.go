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
	reg(registerIfAllowed(server, read, pulseReadTool("list_pulse_subscriptions", "List Pulse Subscriptions", prompts.ListPulseSubscriptionsDescription), pulse.NewListSubscriptionsHandler(client, config)))
	reg(registerIfAllowed(server, read, pulseReadTool("get_pulse_subscription", "Get Pulse Subscription", prompts.GetPulseSubscriptionDescription), pulse.NewGetSubscriptionHandler(client, config)))
	reg(registerIfAllowed(server, manage, pulseWriteTool("create_pulse_subscription", "Create Pulse Subscription", prompts.CreatePulseSubscriptionDescription, false, false), pulse.NewCreateSubscriptionHandler(client, config)))
	reg(registerIfAllowed(server, manage, pulseWriteTool("update_pulse_subscription", "Update Pulse Subscription", prompts.UpdatePulseSubscriptionDescription, true, true), pulse.NewUpdateSubscriptionHandler(client, config)))
	reg(registerIfAllowed(server, manage, pulseWriteTool("enable_pulse_subscription", "Enable Pulse Subscription", prompts.EnablePulseSubscriptionDescription, false, true), pulse.NewEnableSubscriptionHandler(client, config)))
	reg(registerIfAllowed(server, manage, pulseWriteTool("disable_pulse_subscription", "Disable Pulse Subscription", prompts.DisablePulseSubscriptionDescription, false, true), pulse.NewDisableSubscriptionHandler(client, config)))
}

func registerPulseReports(reg func(error), server *last9mcp.Last9MCPServer, config models.Config, client *http.Client) {
	allowed := config.AllowedTools
	reg(registerIfAllowed(server, allowed, pulseReadTool("list_pulse_runs", "List Pulse Runs", prompts.ListPulseRunsDescription), pulse.NewListRunsHandler(client, config)))
	reg(registerIfAllowed(server, allowed, pulseReadTool("get_pulse_run", "Get Pulse Run", prompts.GetPulseRunDescription), pulse.NewGetRunHandler(client, config)))
	reg(registerIfAllowed(server, allowed, pulseReadTool("get_pulse_report", "Get Pulse Report", prompts.GetPulseReportDescription), pulse.NewGetReportHandler(client, config)))
	reg(registerIfAllowed(server, allowed, pulseReadTool("list_pulse_findings", "List Pulse Findings", prompts.ListPulseFindingsDescription), pulse.NewListFindingsHandler(client, config)))
	reg(registerIfAllowed(server, allowed, pulseReadTool("get_pulse_finding", "Get Pulse Finding", prompts.GetPulseFindingDescription), pulse.NewGetFindingHandler(client, config)))
	reg(registerIfAllowed(server, allowed, pulseReadTool("list_pulse_evidence", "List Pulse Evidence", prompts.ListPulseEvidenceDescription), pulse.NewListEvidenceHandler(client, config)))
}

func registerPulseDisposition(reg func(error), server *last9mcp.Last9MCPServer, config models.Config, client *http.Client) {
	tool := pulseWriteTool("write_pulse_disposition", "Write Pulse Disposition", prompts.WritePulseDispositionDescription, false, true)
	reg(registerIfAllowed(server, toolsets.ManageOnly(config.AllowedTools), tool, pulse.NewWriteDispositionHandler(client, config)))
}

func pulseReadTool(name, title, description string) *mcp.Tool {
	return &mcp.Tool{Name: name, Description: description, Annotations: readOnlyTool(title)}
}

func pulseWriteTool(name, title, description string, destructive, idempotent bool) *mcp.Tool {
	return &mcp.Tool{Name: name, Description: description, Annotations: writeTool(title, destructive, idempotent)}
}
