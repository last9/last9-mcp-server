package main

import (
	"context"
	"fmt"

	"last9-mcp/internal/alerting"
	"last9-mcp/internal/apm"
	"last9-mcp/internal/auth"
	"last9-mcp/internal/change_events"
	"last9-mcp/internal/dashboards"
	"last9-mcp/internal/models"
	"last9-mcp/internal/prompts"
	"last9-mcp/internal/suggest"
	"last9-mcp/internal/telemetry/logs"
	"last9-mcp/internal/telemetry/traces"
	"last9-mcp/internal/toolsets"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	last9mcp "github.com/last9/mcp-go-sdk/mcp"
)

// registerIfAllowed registers a tool only when it is in the active toolset set.
// The MCP SDK panics on invalid tool schemas; recover and return an error so
// registerAllTools can fail fast instead of crashing the process.
func registerIfAllowed[In, Out any](server *last9mcp.Last9MCPServer, allowed toolsets.Set, tool *mcp.Tool, handler mcp.ToolHandlerFor[In, Out]) (err error) {
	if !allowed.Allows(tool.Name) {
		return nil
	}
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("register %q: %v", tool.Name, r)
		}
	}()
	if regErr := last9mcp.RegisterInstrumentedTool(server, tool, handler); regErr != nil {
		return fmt.Errorf("register %q: %w", tool.Name, regErr)
	}
	return nil
}

func registerWorkflowPrompt(server *last9mcp.Last9MCPServer, name, title, description, text string) {
	server.Server.AddPrompt(&mcp.Prompt{
		Name:        name,
		Title:       title,
		Description: description,
	}, func(_ context.Context, _ *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		return &mcp.GetPromptResult{
			Description: description,
			Messages: []*mcp.PromptMessage{
				{Role: "user", Content: &mcp.TextContent{Text: text}},
			},
		}, nil
	})
}

func registerWorkflowPrompts(server *last9mcp.Last9MCPServer) {
	registerWorkflowPrompt(
		server,
		"scoped-log-attribute-discovery",
		"Scoped Log Attribute Discovery",
		"Discover service-scoped log fields before building aggregate log filters.",
		prompts.ScopedLogAttributeDiscoveryWorkflow,
	)
	registerWorkflowPrompt(
		server,
		"exception-log-continuation",
		"Exception Log Continuation",
		"Continue exception investigations into aggregate logs to find root-cause signals.",
		prompts.ExceptionLogContinuationWorkflow,
	)
}

// registerAllTools registers all tools with the MCP server using the new SDK pattern
func registerAllTools(server *last9mcp.Last9MCPServer, cfg models.Config) error {
	client := auth.GetHTTPClient()
	registerWorkflowPrompts(server)

	// Whales: short on-tool description only (manuals are MCP resources).
	getLogsDesc := prompts.GetLogsDescription
	getServiceLogsDesc := prompts.GetServiceLogsDescription
	getTracesDesc := prompts.GetTracesDescription
	getServiceTracesDesc := prompts.GetServiceTracesDescription
	// prometheus_range_query: short on-tool description; full guide is MCP resource.
	getMetricsDesc := prompts.PromqlRangeQueryDetails

	var regErr error
	reg := func(err error) {
		if err != nil && regErr == nil {
			regErr = err
		}
	}

	// Register exceptions tool
	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "get_exceptions",
		Description: prompts.GetExceptionsInstructions,
	}, traces.NewGetExceptionsHandler(client, cfg)))

	// Register service summary tool
	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "get_service_summary",
		Description: prompts.GetServiceSummaryDescription,
	}, apm.NewServiceSummaryHandler(client, cfg)))

	// Register APM service deviations tool
	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "get_apm_service_deviations",
		Description: prompts.GetAPMServiceDeviationsDescription,
		InputSchema: apm.GetAPMServiceDeviationsInputSchema(),
	}, apm.NewAPMServiceDeviationsHandler(client, cfg)))

	// Register service environments tool
	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "get_service_environments",
		Description: prompts.GetServiceEnvironmentsDescription,
	}, apm.NewServiceEnvironmentsHandler(client, cfg)))

	// Register service performance details tool
	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "get_service_performance_details",
		Description: prompts.GetServicePerformanceDetails,
	}, apm.NewServicePerformanceDetailsHandler(client, cfg)))

	// Register service operations summary tool
	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "get_service_operations_summary",
		Description: prompts.GetServiceOperationsSummaryDescription,
	}, apm.NewServiceOperationsSummaryHandler(client, cfg)))

	// Register service dependency graph tool
	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "get_service_dependency_graph",
		Description: prompts.GetServiceDependencyGraphDetails,
	}, apm.NewServiceDependencyGraphHandler(client, cfg)))

	// Register list datasources tool
	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "list_datasources",
		Description: prompts.ListDatasourcesDescription,
	}, apm.NewListDatasourcesHandler(cfg)))

	// Register PromQL range query tool (enhanced with metrics instructions)
	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "prometheus_range_query",
		Description: getMetricsDesc,
	}, apm.NewPromqlRangeQueryHandler(client, cfg)))

	// Register PromQL instant query tool
	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "prometheus_instant_query",
		Description: prompts.PromqlInstantQueryDetails,
	}, apm.NewPromqlInstantQueryHandler(client, cfg)))

	// Register PromQL label values tool
	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "prometheus_label_values",
		Description: prompts.PromqlLabelValuesQueryDetails,
	}, apm.NewPromqlLabelValuesHandler(client, cfg)))

	// Register PromQL labels tool
	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "prometheus_labels",
		Description: prompts.PromqlLabelsQueryDetails,
	}, apm.NewPromqlLabelsHandler(client, cfg)))

	// Register logs tool (enhanced with log query instructions + labels)
	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "get_logs",
		Description: getLogsDesc,
	}, logs.NewGetLogsHandler(client, cfg)))

	// Register service logs tool
	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "get_service_logs",
		Description: getServiceLogsDesc,
	}, logs.NewGetServiceLogsHandler(client, cfg)))

	// Register drop rules tool
	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "get_drop_rules",
		Description: prompts.GetDropRulesDescription,
	}, logs.NewGetDropRulesHandler(client, cfg)))

	// Register add drop rule tool
	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "add_drop_rule",
		Description: prompts.AddDropRuleDescription,
	}, logs.NewAddDropRuleHandler(client, cfg)))

	// Register notification channels tool
	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "get_notification_channels",
		Description: prompts.GetNotificationChannelsDescription,
	}, alerting.NewGetNotificationChannelsHandler(client, cfg)))

	// Register alert config tool
	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "get_alert_config",
		Description: prompts.GetAlertConfigDescription,
	}, alerting.NewGetAlertConfigHandler(client, cfg)))

	// Register entity alert rules tool (entity-scoped, includes expression_args and resolved PromQL)
	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "get_entity_alert_rules",
		Description: prompts.GetEntityAlertRulesDescription,
	}, alerting.NewGetEntityAlertRulesHandler(client, cfg)))

	// Register alerts tool
	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "get_alerts",
		Description: prompts.GetAlertsDescription,
	}, alerting.NewGetAlertsHandler(client, cfg)))

	// Register get alert rule state tool
	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "get_alert_rule_state",
		Description: prompts.GetAlertRuleStateDescription,
	}, alerting.NewAlertRuleStateHandler(client, cfg)))

	// Register get traces tool (enhanced with trace query instructions)
	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "get_traces",
		Description: getTracesDesc,
		InputSchema: traces.GetTracesInputSchema(),
	}, traces.NewGetTracesHandler(client, cfg)))

	// Register service traces tool
	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "get_service_traces",
		Description: getServiceTracesDesc,
	}, traces.GetServiceTracesHandler(client, cfg)))

	// Register log attributes tool
	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "get_log_attributes",
		Description: prompts.GetLogAttributesDescription,
	}, logs.NewGetLogAttributesHandler(client, cfg)))

	// Register pipeline-scoped log attributes tool (discovers fields actually
	// present for a given pipeline via the series endpoint)
	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "get_log_attributes_for_pipeline",
		Description: prompts.GetLogAttributesForPipelineDescription,
	}, logs.NewGetLogAttributesForPipelineHandler(client, cfg)))

	// Register trace attributes tool
	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "get_trace_attributes",
		Description: prompts.GetTraceAttributesDescription,
	}, traces.NewGetTraceAttributesHandler(client, cfg)))

	// Register pipeline-scoped trace attributes tool (discovers attributes actually
	// present for a given pipeline via the series endpoint)
	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "get_trace_attributes_for_pipeline",
		Description: prompts.GetTraceAttributesForPipelineDescription,
	}, traces.NewGetTraceAttributesForPipelineHandler(client, cfg)))

	// Register trace attribute values tool
	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "get_trace_attribute_values",
		Description: prompts.GetTraceAttributeValuesDescription,
	}, traces.NewGetTraceAttributeValuesHandler(client, cfg)))

	// Register bounded cohort attribute deviation tool
	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "get_trace_attribute_deviations",
		Description: prompts.GetTraceAttributeDeviationsDescription,
	}, traces.NewGetTraceAttributeDeviationsHandler(client, cfg)))

	// Register exact-trace waterfall tool
	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "get_trace_waterfall",
		Description: prompts.GetTraceWaterfallDescription,
	}, traces.NewGetTraceWaterfallHandler(client, cfg)))

	// Register change events tool
	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "get_change_events",
		Description: prompts.GetChangeEventsDescription,
	}, change_events.NewGetChangeEventsHandler(client, cfg)))

	// Register database discovery tool
	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "get_databases",
		Description: prompts.GetDatabasesDescription,
	}, apm.NewGetDatabasesHandler(client, cfg)))

	// Register database slow queries tool
	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "get_database_slow_queries",
		Description: prompts.GetDatabaseSlowQueriesDescription,
	}, apm.NewGetDatabaseSlowQueriesHandler(client, cfg)))

	// Register database query patterns tool
	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "get_database_queries",
		Description: prompts.GetDatabaseQueriesDescription,
	}, apm.NewGetDatabaseQueriesHandler(client, cfg)))

	// Register database server-side metrics tool
	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "get_database_server_metrics",
		Description: prompts.GetDatabaseServerMetricsDescription,
	}, apm.NewGetDatabaseServerMetricsHandler(client, cfg)))

	// Register did_you_mean tool
	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "did_you_mean",
		Description: prompts.DidYouMeanDescription,
	}, suggest.NewDidYouMeanHandler(client, cfg)))

	// Register dashboard tools
	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "list_dashboards",
		Description: prompts.ListDashboardsDescription,
	}, dashboards.NewListDashboardsHandler(client, cfg)))

	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "get_dashboard",
		Description: prompts.GetDashboardDescription,
	}, dashboards.NewGetDashboardHandler(client, cfg)))

	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "create_dashboard",
		Description: prompts.CreateDashboardDescription,
		InputSchema: dashboards.GetCreateDashboardInputSchema(),
	}, dashboards.NewCreateDashboardHandler(client, cfg)))

	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "update_dashboard",
		Description: prompts.UpdateDashboardDescription,
		InputSchema: dashboards.GetUpdateDashboardInputSchema(),
	}, dashboards.NewUpdateDashboardHandler(client, cfg)))

	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "delete_dashboard",
		Description: prompts.DeleteDashboardDescription,
	}, dashboards.NewDeleteDashboardHandler(client, cfg)))

	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "list_dashboard_snapshots",
		Description: prompts.ListDashboardSnapshotsDescription,
	}, dashboards.NewListDashboardSnapshotsHandler(client, cfg)))

	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "get_dashboard_snapshot",
		Description: prompts.GetDashboardSnapshotDescription,
	}, dashboards.NewGetDashboardSnapshotHandler(client, cfg)))

	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "delete_dashboard_snapshot",
		Description: prompts.DeleteDashboardSnapshotDescription,
	}, dashboards.NewDeleteDashboardSnapshotHandler(client, cfg)))

	return regErr
}
