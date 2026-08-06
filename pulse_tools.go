package main

import (
	"fmt"
	"net/http"

	"last9-mcp/internal/models"
	"last9-mcp/internal/prompts"
	"last9-mcp/internal/pulse"
	"last9-mcp/internal/toolsets"

	last9mcp "github.com/last9/mcp-go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func registerPulseTools(server *last9mcp.Last9MCPServer, config models.Config, client *http.Client) error {
	for _, register := range []func() error{
		func() error { return registerPulseSubscriptions(server, config, client) },
		func() error { return registerPulseReports(server, config, client) },
		func() error { return registerPulseDisposition(server, config, client) },
	} {
		if err := register(); err != nil {
			return err
		}
	}
	return nil
}

func registerPulseSubscriptions(server *last9mcp.Last9MCPServer, config models.Config, client *http.Client) error {
	read := config.AllowedTools
	manage := toolsets.ManageOnly(config.AllowedTools)
	registrations := []func() error{
		func() error {
			return registerIfAllowed(server, read, pulseReadTool("list_pulse_subscriptions", "List subscriptions", prompts.PulseSubscriptionsDescription), pulse.NewListSubscriptionsHandler(client, config))
		},
		func() error {
			return registerIfAllowed(server, read, pulseReadTool("get_pulse_subscription", "Get subscription", prompts.PulseSubscriptionsDescription), pulse.NewGetSubscriptionHandler(client, config))
		},
		func() error {
			return registerIfAllowed(server, manage, pulseWriteTool("create_pulse_subscription", "Create disabled subscription", prompts.PulseSubscriptionsDescription), pulse.NewCreateSubscriptionHandler(client, config))
		},
		func() error {
			return registerIfAllowed(server, manage, pulseWriteTool("update_pulse_subscription", "Update subscription", prompts.PulseSubscriptionsDescription), pulse.NewUpdateSubscriptionHandler(client, config))
		},
		func() error {
			return registerIfAllowed(server, manage, pulseWriteTool("enable_pulse_subscription", "Enable subscription", prompts.PulseSubscriptionsDescription), pulse.NewEnableSubscriptionHandler(client, config))
		},
		func() error {
			return registerIfAllowed(server, manage, pulseWriteTool("disable_pulse_subscription", "Disable subscription", prompts.PulseSubscriptionsDescription), pulse.NewDisableSubscriptionHandler(client, config))
		},
	}
	return runRegistrations(registrations)
}

func registerPulseReports(server *last9mcp.Last9MCPServer, config models.Config, client *http.Client) error {
	allowed := config.AllowedTools
	registrations := []func() error{
		func() error {
			return registerIfAllowed(server, allowed, pulseReadTool("list_pulse_runs", "List run status", prompts.PulseReportsDescription), pulse.NewListRunsHandler(client, config))
		},
		func() error {
			return registerIfAllowed(server, allowed, pulseReadTool("get_pulse_run", "Get run status", prompts.PulseReportsDescription), pulse.NewGetRunHandler(client, config))
		},
		func() error {
			return registerIfAllowed(server, allowed, pulseReadTool("get_pulse_report", "Get report", prompts.PulseReportsDescription), pulse.NewGetReportHandler(client, config))
		},
		func() error {
			return registerIfAllowed(server, allowed, pulseReadTool("list_pulse_findings", "List findings", prompts.PulseReportsDescription), pulse.NewListFindingsHandler(client, config))
		},
		func() error {
			return registerIfAllowed(server, allowed, pulseReadTool("get_pulse_finding", "Get finding detail", prompts.PulseReportsDescription), pulse.NewGetFindingHandler(client, config))
		},
		func() error {
			return registerIfAllowed(server, allowed, pulseReadTool("list_pulse_evidence", "List safe evidence", prompts.PulseReportsDescription), pulse.NewListEvidenceHandler(client, config))
		},
	}
	return runRegistrations(registrations)
}

func registerPulseDisposition(server *last9mcp.Last9MCPServer, config models.Config, client *http.Client) error {
	tool := pulseWriteTool("write_pulse_disposition", "Write disposition", prompts.PulseDispositionsDescription)
	return registerIfAllowed(server, toolsets.ManageOnly(config.AllowedTools), tool, pulse.NewWriteDispositionHandler(client, config))
}

func runRegistrations(registrations []func() error) error {
	for _, register := range registrations {
		if err := register(); err != nil {
			return err
		}
	}
	return nil
}

func pulseReadTool(name string, action string, description string) *mcp.Tool {
	return &mcp.Tool{Name: name, Description: fmt.Sprintf("%s.\n\n%s", action, description), Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}
}

func pulseWriteTool(name string, action string, description string) *mcp.Tool {
	nonDestructive := false
	return &mcp.Tool{Name: name, Description: fmt.Sprintf("%s.\n\n%s", action, description), Annotations: &mcp.ToolAnnotations{DestructiveHint: &nonDestructive}}
}
