package main

import (
	"strings"

	"last9-mcp/internal/alerting"
	"last9-mcp/internal/apm"
	"last9-mcp/internal/attributes"
	"last9-mcp/internal/auth"
	"last9-mcp/internal/change_events"
	"last9-mcp/internal/dashboards"
	"last9-mcp/internal/models"
	"last9-mcp/internal/paramhint"
	"last9-mcp/internal/prompts"
	"last9-mcp/internal/suggest"
	"last9-mcp/internal/telemetry/logs"
	"last9-mcp/internal/telemetry/traces"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	last9mcp "github.com/last9/mcp-go-sdk/mcp"
)

// buildEnhancedDescription appends the embedded markdown instructions to the
// base tool description. For get_logs, it also replaces the {{labels}} placeholder
// with the actual attribute names from the cache.
func buildEnhancedDescription(base, instructions string, labelValues []string) string {
	desc := base + "\n\n" + instructions
	if len(labelValues) > 0 {
		desc = strings.ReplaceAll(desc, "{{labels}}", strings.Join(labelValues, ", "))
	} else {
		desc = strings.ReplaceAll(desc, "{{labels}}", "")
	}
	return desc
}

// paramRegistry is shared between tool registration (writes) and the
// paramhint middleware (reads). Package-level because registerAllTools
// re-runs every 2h to refresh tool descriptions (see main.go): the
// middleware must be attached exactly once per server — attaching it inside
// registerAllTools would stack one copy per refresh and append duplicate
// hints — while re-registration only rewrites the same names into the
// existing registry. Shared across all servers in the process (tests create
// several); safe because every server registers the identical tool set and
// Register is last-write-wins idempotent for identical inputs.
var paramRegistry = paramhint.NewRegistry()

// AttachParamHintMiddleware wires the -32602 enrichment middleware to a
// server. Call exactly once per server, before serving.
func AttachParamHintMiddleware(server *last9mcp.Last9MCPServer) {
	server.Server.AddReceivingMiddleware(paramhint.Middleware(paramRegistry))
}

// registerTool registers a tool and records its valid parameter names so the
// paramhint middleware can build recovery hints for schema-validation errors.
func registerTool[In, Out any](server *last9mcp.Last9MCPServer, reg *paramhint.Registry, tool *mcp.Tool, handler mcp.ToolHandlerFor[In, Out]) error {
	reg.Register(tool.Name, paramhint.ParamsOf[In]())
	return last9mcp.RegisterInstrumentedTool(server, tool, handler)
}

// registerAllTools registers all tools with the MCP server using the new SDK pattern
func registerAllTools(server *last9mcp.Last9MCPServer, cfg models.Config, attrCache *attributes.AttributeCache) error {
	client := auth.GetHTTPClient()
	reg := paramRegistry

	// Build enhanced descriptions for tools that have embedded instructions
	getLogsDesc := buildEnhancedDescription(prompts.GetLogsDescription, prompts.GetLogsInstructions, attrCache.GetLogAttributes())
	getServiceLogsDesc := buildEnhancedDescription(prompts.GetServiceLogsDescription, prompts.GetServiceLogsInstructions, attrCache.GetLogAttributes())
	getTracesDesc := buildEnhancedDescription(prompts.GetTracesDescription, prompts.GetTracesInstructions, nil)
	getServiceTracesDesc := buildEnhancedDescription(prompts.GetServiceTracesDescription, prompts.GetServiceTracesInstructions, nil)
	getMetricsDesc := buildEnhancedDescription(prompts.PromqlRangeQueryDetails, prompts.GetMetricsInstructions, nil)

	// Register exceptions tool
	registerTool(server, reg, &mcp.Tool{
		Name:        "get_exceptions",
		Description: prompts.GetExceptionsInstructions,
	}, traces.NewGetExceptionsHandler(client, cfg))

	// Register service summary tool
	registerTool(server, reg, &mcp.Tool{
		Name:        "get_service_summary",
		Description: prompts.GetServiceSummaryDescription,
	}, apm.NewServiceSummaryHandler(client, cfg))

	// Register service environments tool
	registerTool(server, reg, &mcp.Tool{
		Name:        "get_service_environments",
		Description: prompts.GetServiceEnvironmentsDescription,
	}, apm.NewServiceEnvironmentsHandler(client, cfg))

	// Register service performance details tool
	registerTool(server, reg, &mcp.Tool{
		Name:        "get_service_performance_details",
		Description: prompts.GetServicePerformanceDetails,
	}, apm.NewServicePerformanceDetailsHandler(client, cfg))

	// Register service operations summary tool
	registerTool(server, reg, &mcp.Tool{
		Name:        "get_service_operations_summary",
		Description: prompts.GetServiceOperationsSummaryDescription,
	}, apm.NewServiceOperationsSummaryHandler(client, cfg))

	// Register service dependency graph tool
	registerTool(server, reg, &mcp.Tool{
		Name:        "get_service_dependency_graph",
		Description: prompts.GetServiceDependencyGraphDetails,
	}, apm.NewServiceDependencyGraphHandler(client, cfg))

	// Register list datasources tool
	registerTool(server, reg, &mcp.Tool{
		Name:        "list_datasources",
		Description: prompts.ListDatasourcesDescription,
	}, apm.NewListDatasourcesHandler(cfg))

	// Register PromQL range query tool (enhanced with metrics instructions)
	registerTool(server, reg, &mcp.Tool{
		Name:        "prometheus_range_query",
		Description: getMetricsDesc,
	}, apm.NewPromqlRangeQueryHandler(client, cfg))

	// Register PromQL instant query tool
	registerTool(server, reg, &mcp.Tool{
		Name:        "prometheus_instant_query",
		Description: prompts.PromqlInstantQueryDetails,
	}, apm.NewPromqlInstantQueryHandler(client, cfg))

	// Register PromQL label values tool
	registerTool(server, reg, &mcp.Tool{
		Name:        "prometheus_label_values",
		Description: prompts.PromqlLabelValuesQueryDetails,
	}, apm.NewPromqlLabelValuesHandler(client, cfg))

	// Register PromQL labels tool
	registerTool(server, reg, &mcp.Tool{
		Name:        "prometheus_labels",
		Description: prompts.PromqlLabelsQueryDetails,
	}, apm.NewPromqlLabelsHandler(client, cfg))

	// Register logs tool (enhanced with log query instructions + labels)
	registerTool(server, reg, &mcp.Tool{
		Name:        "get_logs",
		Description: getLogsDesc,
	}, logs.NewGetLogsHandler(client, cfg))

	// Register service logs tool
	registerTool(server, reg, &mcp.Tool{
		Name:        "get_service_logs",
		Description: getServiceLogsDesc,
	}, logs.NewGetServiceLogsHandler(client, cfg))

	// Register drop rules tool
	registerTool(server, reg, &mcp.Tool{
		Name:        "get_drop_rules",
		Description: prompts.GetDropRulesDescription,
	}, logs.NewGetDropRulesHandler(client, cfg))

	// Register add drop rule tool
	registerTool(server, reg, &mcp.Tool{
		Name:        "add_drop_rule",
		Description: prompts.AddDropRuleDescription,
	}, logs.NewAddDropRuleHandler(client, cfg))

	// Register notification channels tool
	registerTool(server, reg, &mcp.Tool{
		Name:        "get_notification_channels",
		Description: prompts.GetNotificationChannelsDescription,
	}, alerting.NewGetNotificationChannelsHandler(client, cfg))

	// Register alert config tool
	registerTool(server, reg, &mcp.Tool{
		Name:        "get_alert_config",
		Description: prompts.GetAlertConfigDescription,
	}, alerting.NewGetAlertConfigHandler(client, cfg))

	// Register entity alert rules tool (entity-scoped, includes expression_args and resolved PromQL)
	registerTool(server, reg, &mcp.Tool{
		Name:        "get_entity_alert_rules",
		Description: prompts.GetEntityAlertRulesDescription,
	}, alerting.NewGetEntityAlertRulesHandler(client, cfg))

	// Register alerts tool
	registerTool(server, reg, &mcp.Tool{
		Name:        "get_alerts",
		Description: prompts.GetAlertsDescription,
	}, alerting.NewGetAlertsHandler(client, cfg))

	// Register get alert rule state tool
	registerTool(server, reg, &mcp.Tool{
		Name:        "get_alert_rule_state",
		Description: prompts.GetAlertRuleStateDescription,
	}, alerting.NewAlertRuleStateHandler(client, cfg))

	// Register get traces tool (enhanced with trace query instructions)
	registerTool(server, reg, &mcp.Tool{
		Name:        "get_traces",
		Description: getTracesDesc,
		InputSchema: traces.GetTracesInputSchema(),
	}, traces.NewGetTracesHandler(client, cfg))

	// Register service traces tool
	registerTool(server, reg, &mcp.Tool{
		Name:        "get_service_traces",
		Description: getServiceTracesDesc,
	}, traces.GetServiceTracesHandler(client, cfg))

	// Register log attributes tool
	registerTool(server, reg, &mcp.Tool{
		Name:        "get_log_attributes",
		Description: prompts.GetLogAttributesDescription,
	}, logs.NewGetLogAttributesHandler(client, cfg))

	// Register pipeline-scoped log attributes tool (discovers fields actually
	// present for a given pipeline via the series endpoint)
	registerTool(server, reg, &mcp.Tool{
		Name:        "get_log_attributes_for_pipeline",
		Description: prompts.GetLogAttributesForPipelineDescription,
	}, logs.NewGetLogAttributesForPipelineHandler(client, cfg))

	// Register trace attributes tool
	registerTool(server, reg, &mcp.Tool{
		Name:        "get_trace_attributes",
		Description: prompts.GetTraceAttributesDescription,
	}, traces.NewGetTraceAttributesHandler(client, cfg))

	// Register pipeline-scoped trace attributes tool (discovers attributes actually
	// present for a given pipeline via the series endpoint)
	last9mcp.RegisterInstrumentedTool(server, &mcp.Tool{
		Name:        "get_trace_attributes_for_pipeline",
		Description: traces.GetTraceAttributesForPipelineDescription,
	}, traces.NewGetTraceAttributesForPipelineHandler(client, cfg))

	// Register pipeline-scoped trace attributes tool (discovers attributes actually
	// present for a given pipeline via the series endpoint)
	last9mcp.RegisterInstrumentedTool(server, &mcp.Tool{
		Name:        "get_trace_attributes_for_pipeline",
		Description: traces.GetTraceAttributesForPipelineDescription,
	}, traces.NewGetTraceAttributesForPipelineHandler(client, cfg))

	// Register trace attribute values tool
	registerTool(server, reg, &mcp.Tool{
		Name:        "get_trace_attribute_values",
		Description: prompts.GetTraceAttributeValuesDescription,
	}, traces.NewGetTraceAttributeValuesHandler(client, cfg))

	// Register change events tool
	registerTool(server, reg, &mcp.Tool{
		Name:        "get_change_events",
		Description: prompts.GetChangeEventsDescription,
	}, change_events.NewGetChangeEventsHandler(client, cfg))

	// Register database discovery tool
	registerTool(server, reg, &mcp.Tool{
		Name:        "get_databases",
		Description: prompts.GetDatabasesDescription,
	}, apm.NewGetDatabasesHandler(client, cfg))

	// Register database slow queries tool
	registerTool(server, reg, &mcp.Tool{
		Name:        "get_database_slow_queries",
		Description: prompts.GetDatabaseSlowQueriesDescription,
	}, apm.NewGetDatabaseSlowQueriesHandler(client, cfg))

	// Register database query patterns tool
	registerTool(server, reg, &mcp.Tool{
		Name:        "get_database_queries",
		Description: prompts.GetDatabaseQueriesDescription,
	}, apm.NewGetDatabaseQueriesHandler(client, cfg))

	// Register database server-side metrics tool
	registerTool(server, reg, &mcp.Tool{
		Name:        "get_database_server_metrics",
		Description: prompts.GetDatabaseServerMetricsDescription,
	}, apm.NewGetDatabaseServerMetricsHandler(client, cfg))

	// Register did_you_mean tool
	registerTool(server, reg, &mcp.Tool{
		Name:        "did_you_mean",
		Description: prompts.DidYouMeanDescription,
	}, suggest.NewDidYouMeanHandler(client, cfg))

	// Register dashboard tools
	registerTool(server, reg, &mcp.Tool{
		Name:        "list_dashboards",
		Description: prompts.ListDashboardsDescription,
	}, dashboards.NewListDashboardsHandler(client, cfg))

	registerTool(server, reg, &mcp.Tool{
		Name:        "get_dashboard",
		Description: prompts.GetDashboardDescription,
	}, dashboards.NewGetDashboardHandler(client, cfg))

	registerTool(server, reg, &mcp.Tool{
		Name:        "create_dashboard",
		Description: prompts.CreateDashboardDescription,
		InputSchema: dashboards.GetCreateDashboardInputSchema(),
	}, dashboards.NewCreateDashboardHandler(client, cfg))

	registerTool(server, reg, &mcp.Tool{
		Name:        "update_dashboard",
		Description: prompts.UpdateDashboardDescription,
		InputSchema: dashboards.GetUpdateDashboardInputSchema(),
	}, dashboards.NewUpdateDashboardHandler(client, cfg))

	registerTool(server, reg, &mcp.Tool{
		Name:        "delete_dashboard",
		Description: prompts.DeleteDashboardDescription,
	}, dashboards.NewDeleteDashboardHandler(client, cfg))

	return nil
}
