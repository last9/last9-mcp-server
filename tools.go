package main

import (
	"fmt"

	"last9-mcp/internal/alerting"
	"last9-mcp/internal/apm"
	"last9-mcp/internal/auth"
	"last9-mcp/internal/change_events"
	"last9-mcp/internal/dashboards"
	"last9-mcp/internal/grafana"
	"last9-mcp/internal/models"
	"last9-mcp/internal/prompts"
	"last9-mcp/internal/suggest"
	"last9-mcp/internal/telemetry/logs"
	"last9-mcp/internal/telemetry/profiles"
	"last9-mcp/internal/telemetry/traces"
	"last9-mcp/internal/toolsets"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	last9mcp "github.com/last9/mcp-go-sdk/mcp"
)

// Tool annotations drive client permissions: a client may run a read-only tool
// without asking, and always confirms a destructive one. Every tool acts only on
// the caller's own Last9 organization, so none is open-world.
func readOnlyTool(title string) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{Title: title, ReadOnlyHint: true, OpenWorldHint: boolPtr(false)}
}

// writeTool annotates a tool that changes Last9 state. destructive marks tools
// that overwrite or remove data; idempotent marks tools where repeating a call
// with the same arguments has no further effect.
func writeTool(title string, destructive, idempotent bool) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{
		Title:           title,
		DestructiveHint: boolPtr(destructive),
		IdempotentHint:  idempotent,
		OpenWorldHint:   boolPtr(false),
	}
}

func boolPtr(b bool) *bool { return &b }

// registerIfAllowed registers a tool only when it is in the active toolset set.
// The MCP SDK panics on invalid tool schemas; recover and return an error so
// registerAllTools can fail fast instead of crashing the process.
func registerIfAllowed[In, Out any](server *last9mcp.Last9MCPServer, allowed toolsets.Set, tool *mcp.Tool, handler mcp.ToolHandlerFor[In, Out]) (err error) {
	if !allowed.Allows(tool.Name) {
		return nil
	}
	// Serve the annotation title as the tool's title too, so clients that read
	// either field show the same name.
	if tool.Title == "" && tool.Annotations != nil {
		tool.Title = tool.Annotations.Title
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

// registerAllTools registers all tools with the MCP server using the new SDK pattern
func registerAllTools(server *last9mcp.Last9MCPServer, cfg models.Config) error {
	client := auth.GetHTTPClient()

	var regErr error
	reg := func(err error) {
		if err != nil && regErr == nil {
			regErr = err
		}
	}

	// Register exceptions tool
	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "get_exceptions",
		Annotations: readOnlyTool("Get Exceptions"),
		Description: prompts.GetExceptionsInstructions,
	}, traces.NewGetExceptionsHandler(client, cfg)))

	// Register service summary tool
	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "get_service_summary",
		Annotations: readOnlyTool("Get Service Summary"),
		Description: prompts.GetServiceSummaryDescription,
		InputSchema: apm.GetServiceSummaryInputSchema(),
	}, apm.NewServiceSummaryHandler(client, cfg)))

	// Register APM service deviations tool
	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "get_apm_service_deviations",
		Annotations: readOnlyTool("Get APM Service Deviations"),
		Description: prompts.GetAPMServiceDeviationsDescription,
		InputSchema: apm.GetAPMServiceDeviationsInputSchema(),
	}, apm.NewAPMServiceDeviationsHandler(client, cfg)))

	// Register service environments tool
	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "get_service_environments",
		Annotations: readOnlyTool("Get Service Environments"),
		Description: prompts.GetServiceEnvironmentsDescription,
	}, apm.NewServiceEnvironmentsHandler(client, cfg)))

	// Register service performance details tool
	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "get_service_performance_details",
		Annotations: readOnlyTool("Get Service Performance Details"),
		Description: prompts.GetServicePerformanceDetails,
	}, apm.NewServicePerformanceDetailsHandler(client, cfg)))

	// Register service operations summary tool
	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "get_service_operations_summary",
		Annotations: readOnlyTool("Get Service Operations Summary"),
		Description: prompts.GetServiceOperationsSummaryDescription,
	}, apm.NewServiceOperationsSummaryHandler(client, cfg)))

	// Register service dependency graph tool
	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "get_service_dependency_graph",
		Annotations: readOnlyTool("Get Service Dependency Graph"),
		Description: prompts.GetServiceDependencyGraphDetails,
	}, apm.NewServiceDependencyGraphHandler(client, cfg)))

	// Register list datasources tool
	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "list_datasources",
		Annotations: readOnlyTool("List Datasources"),
		Description: prompts.ListDatasourcesDescription,
	}, apm.NewListDatasourcesHandler(cfg)))

	// Register PromQL range query tool (enhanced with metrics instructions)
	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "prometheus_range_query",
		Annotations: readOnlyTool("Run PromQL Range Query"),
		Description: prompts.PromqlRangeQueryDetails,
	}, apm.NewPromqlRangeQueryHandler(client, cfg)))

	// Register PromQL instant query tool
	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "prometheus_instant_query",
		Annotations: readOnlyTool("Run PromQL Instant Query"),
		Description: prompts.PromqlInstantQueryDetails,
	}, apm.NewPromqlInstantQueryHandler(client, cfg)))

	// Register PromQL label values tool
	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "prometheus_label_values",
		Annotations: readOnlyTool("Get Prometheus Label Values"),
		Description: prompts.PromqlLabelValuesQueryDetails,
	}, apm.NewPromqlLabelValuesHandler(client, cfg)))

	// Register PromQL labels tool
	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "prometheus_labels",
		Annotations: readOnlyTool("Get Prometheus Labels"),
		Description: prompts.PromqlLabelsQueryDetails,
	}, apm.NewPromqlLabelsHandler(client, cfg)))

	// Register logs tool (enhanced with log query instructions + labels)
	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "get_logs",
		Annotations: readOnlyTool("Get Logs"),
		Description: prompts.GetLogsDescription,
		InputSchema: logs.GetLogsInputSchema(),
	}, logs.NewGetLogsHandler(client, cfg)))

	// Register service logs tool
	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "get_service_logs",
		Annotations: readOnlyTool("Get Service Logs"),
		Description: prompts.GetServiceLogsDescription,
	}, logs.NewGetServiceLogsHandler(client, cfg)))

	// Register drop rules tool
	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "get_drop_rules",
		Annotations: readOnlyTool("Get Log Drop Rules"),
		Description: prompts.GetDropRulesDescription,
	}, logs.NewGetDropRulesHandler(client, cfg)))

	// Register add drop rule tool
	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "add_drop_rule",
		Annotations: writeTool("Add Log Drop Rule", true, false),
		Description: prompts.AddDropRuleDescription,
	}, logs.NewAddDropRuleHandler(client, cfg)))

	// Register notification channels tool
	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "get_notification_channels",
		Annotations: readOnlyTool("Get Notification Channels"),
		Description: prompts.GetNotificationChannelsDescription,
	}, alerting.NewGetNotificationChannelsHandler(client, cfg)))

	// Register alert config tool
	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "get_alert_config",
		Annotations: readOnlyTool("Get Alert Config"),
		Description: prompts.GetAlertConfigDescription,
	}, alerting.NewGetAlertConfigHandler(client, cfg)))

	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "get_alert_groups",
		Annotations: readOnlyTool("Get Alert Groups"),
		Description: prompts.GetAlertGroupsDescription,
	}, alerting.NewGetAlertGroupsHandler(client, cfg)))

	// Register entity alert rules tool (entity-scoped, includes expression_args and resolved PromQL)
	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "get_entity_alert_rules",
		Annotations: readOnlyTool("Get Entity Alert Rules"),
		Description: prompts.GetEntityAlertRulesDescription,
	}, alerting.NewGetEntityAlertRulesHandler(client, cfg)))

	// Register alerts tool
	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "get_alerts",
		Annotations: readOnlyTool("Get Alerts"),
		Description: prompts.GetAlertsDescription,
	}, alerting.NewGetAlertsHandler(client, cfg)))

	// Register get alert rule state tool
	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "get_alert_rule_state",
		Annotations: readOnlyTool("Get Alert Rule State"),
		Description: prompts.GetAlertRuleStateDescription,
	}, alerting.NewAlertRuleStateHandler(client, cfg)))

	// Register get traces tool (enhanced with trace query instructions)
	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "get_traces",
		Annotations: readOnlyTool("Get Traces"),
		Description: prompts.GetTracesDescription,
		InputSchema: traces.GetTracesInputSchema(),
	}, traces.NewGetTracesHandler(client, cfg)))

	// Register service traces tool
	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "get_service_traces",
		Annotations: readOnlyTool("Get Service Traces"),
		Description: prompts.GetServiceTracesDescription,
	}, traces.GetServiceTracesHandler(client, cfg)))

	// Register log attributes tool
	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "get_log_attributes",
		Annotations: readOnlyTool("Get Log Attributes"),
		Description: prompts.GetLogAttributesDescription,
	}, logs.NewGetLogAttributesHandler(client, cfg)))

	// Register pipeline-scoped log attributes tool (discovers fields actually
	// present for a given pipeline via the series endpoint)
	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "get_log_attributes_for_pipeline",
		Annotations: readOnlyTool("Get Log Attributes for Pipeline"),
		Description: prompts.GetLogAttributesForPipelineDescription,
	}, logs.NewGetLogAttributesForPipelineHandler(client, cfg)))

	// Register trace attributes tool
	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "get_trace_attributes",
		Annotations: readOnlyTool("Get Trace Attributes"),
		Description: prompts.GetTraceAttributesDescription,
	}, traces.NewGetTraceAttributesHandler(client, cfg)))

	// Register pipeline-scoped trace attributes tool (discovers attributes actually
	// present for a given pipeline via the series endpoint)
	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "get_trace_attributes_for_pipeline",
		Annotations: readOnlyTool("Get Trace Attributes for Pipeline"),
		Description: prompts.GetTraceAttributesForPipelineDescription,
	}, traces.NewGetTraceAttributesForPipelineHandler(client, cfg)))

	// Register trace attribute values tool
	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "get_trace_attribute_values",
		Annotations: readOnlyTool("Get Trace Attribute Values"),
		Description: prompts.GetTraceAttributeValuesDescription,
	}, traces.NewGetTraceAttributeValuesHandler(client, cfg)))

	// Register bounded cohort attribute deviation tool
	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "get_trace_attribute_deviations",
		Annotations: readOnlyTool("Get Trace Attribute Deviations"),
		Description: prompts.GetTraceAttributeDeviationsDescription,
	}, traces.NewGetTraceAttributeDeviationsHandler(client, cfg)))

	// Register exact-trace waterfall tool
	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "get_trace_waterfall",
		Annotations: readOnlyTool("Get Trace Waterfall"),
		Description: prompts.GetTraceWaterfallDescription,
		InputSchema: traces.GetTraceWaterfallInputSchema(),
	}, traces.NewGetTraceWaterfallHandler(client, cfg)))

	// Register change events tool
	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "get_change_events",
		Annotations: readOnlyTool("Get Change Events"),
		Description: prompts.GetChangeEventsDescription,
	}, change_events.NewGetChangeEventsHandler(client, cfg)))

	// Register database discovery tool
	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "get_databases",
		Annotations: readOnlyTool("Get Databases"),
		Description: prompts.GetDatabasesDescription,
	}, apm.NewGetDatabasesHandler(client, cfg)))

	// Register database slow queries tool
	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "get_database_slow_queries",
		Annotations: readOnlyTool("Get Database Slow Queries"),
		Description: prompts.GetDatabaseSlowQueriesDescription,
	}, apm.NewGetDatabaseSlowQueriesHandler(client, cfg)))

	// Register database query patterns tool
	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "get_database_queries",
		Annotations: readOnlyTool("Get Database Queries"),
		Description: prompts.GetDatabaseQueriesDescription,
	}, apm.NewGetDatabaseQueriesHandler(client, cfg)))

	// Register database server-side metrics tool
	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "get_database_server_metrics",
		Annotations: readOnlyTool("Get Database Server Metrics"),
		Description: prompts.GetDatabaseServerMetricsDescription,
	}, apm.NewGetDatabaseServerMetricsHandler(client, cfg)))

	// Register did_you_mean tool
	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "did_you_mean",
		Annotations: readOnlyTool("Suggest Matching Names"),
		Description: prompts.DidYouMeanDescription,
	}, suggest.NewDidYouMeanHandler(client, cfg)))

	// Register service profile tool
	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "get_service_profile",
		Annotations: readOnlyTool("Get Service Profile"),
		Description: prompts.GetServiceProfileDescription,
	}, apm.NewGetServiceProfileHandler(client, cfg)))

	// Continuous profiling tools (query_range/json)
	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "get_profile_services",
		Annotations: readOnlyTool("Get Profiled Services"),
		Description: prompts.GetProfileServicesDescription,
	}, profiles.NewGetProfileServicesHandler(client, cfg)))

	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "get_flamegraph",
		Annotations: readOnlyTool("Get Flame Graph"),
		Description: prompts.GetFlamegraphDescription,
	}, profiles.NewGetFlamegraphHandler(client, cfg)))

	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "get_top_functions",
		Annotations: readOnlyTool("Get Top Functions"),
		Description: prompts.GetTopFunctionsDescription,
	}, profiles.NewGetTopFunctionsHandler(client, cfg)))

	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "get_profile_summary",
		Annotations: readOnlyTool("Get Profile Summary"),
		Description: prompts.GetProfileSummaryDescription,
	}, profiles.NewGetProfileSummaryHandler(client, cfg)))

	// Register dashboard tools
	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "list_dashboards",
		Annotations: readOnlyTool("List Dashboards"),
		Description: prompts.ListDashboardsDescription,
	}, dashboards.NewListDashboardsHandler(client, cfg)))

	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "get_dashboard",
		Annotations: readOnlyTool("Get Dashboard"),
		Description: prompts.GetDashboardDescription,
	}, dashboards.NewGetDashboardHandler(client, cfg)))

	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "validate_dashboard",
		Annotations: readOnlyTool("Validate Dashboard"),
		Description: prompts.ValidateDashboardDescription,
		InputSchema: dashboards.GetValidateDashboardInputSchema(),
	}, dashboards.NewValidateDashboardHandler(client, cfg)))

	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "create_dashboard",
		Annotations: writeTool("Create Dashboard", false, false),
		Description: prompts.CreateDashboardDescription,
		InputSchema: dashboards.GetCreateDashboardInputSchema(),
	}, dashboards.NewCreateDashboardHandler(client, cfg)))

	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "update_dashboard",
		Annotations: writeTool("Update Dashboard", true, true),
		Description: prompts.UpdateDashboardDescription,
		InputSchema: dashboards.GetUpdateDashboardInputSchema(),
	}, dashboards.NewUpdateDashboardHandler(client, cfg)))

	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "delete_dashboard",
		Annotations: writeTool("Delete Dashboard", true, true),
		Description: prompts.DeleteDashboardDescription,
	}, dashboards.NewDeleteDashboardHandler(client, cfg)))

	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "list_dashboard_snapshots",
		Annotations: readOnlyTool("List Dashboard Snapshots"),
		Description: prompts.ListDashboardSnapshotsDescription,
	}, dashboards.NewListDashboardSnapshotsHandler(client, cfg)))

	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "get_dashboard_snapshot",
		Annotations: readOnlyTool("Get Dashboard Snapshot"),
		Description: prompts.GetDashboardSnapshotDescription,
	}, dashboards.NewGetDashboardSnapshotHandler(client, cfg)))

	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "delete_dashboard_snapshot",
		Annotations: writeTool("Delete Dashboard Snapshot", true, true),
		Description: prompts.DeleteDashboardSnapshotDescription,
	}, dashboards.NewDeleteDashboardSnapshotHandler(client, cfg)))

	// Register Grafana read tools
	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "grafana_search_dashboards",
		Annotations: readOnlyTool("Search Grafana Dashboards"),
		Description: prompts.GrafanaSearchDashboardsDescription,
	}, grafana.NewSearchDashboardsHandler(client, cfg)))

	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "grafana_get_dashboard",
		Annotations: readOnlyTool("Get Grafana Dashboard"),
		Description: prompts.GrafanaGetDashboardDescription,
	}, grafana.NewGetDashboardHandler(client, cfg)))

	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "grafana_list_folders",
		Annotations: readOnlyTool("List Grafana Folders"),
		Description: prompts.GrafanaListFoldersDescription,
	}, grafana.NewListFoldersHandler(client, cfg)))

	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "grafana_list_folder_dashboards",
		Annotations: readOnlyTool("List Grafana Folder Dashboards"),
		Description: prompts.GrafanaListFolderDashboardsDescription,
	}, grafana.NewListFolderDashboardsHandler(client, cfg)))

	reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{
		Name:        "grafana_list_datasources",
		Annotations: readOnlyTool("List Grafana Datasources"),
		Description: prompts.GrafanaListDatasourcesDescription,
	}, grafana.NewListDatasourcesHandler(client, cfg)))

	return regErr
}
