package main

import (
	"strings"

	"last9-mcp/internal/alerting"
	"last9-mcp/internal/apm"
	"last9-mcp/internal/attributes"
	"last9-mcp/internal/auth"
	"last9-mcp/internal/change_events"
	"last9-mcp/internal/discovery"
	"last9-mcp/internal/models"
	"last9-mcp/internal/prompts"
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

// registerAllTools registers all tools with the MCP server using the new SDK pattern
func registerAllTools(server *last9mcp.Last9MCPServer, cfg models.Config, attrCache *attributes.AttributeCache) error {
	client := auth.GetHTTPClient()

	// Build enhanced descriptions for tools that have embedded instructions
	getLogsDesc := buildEnhancedDescription(logs.GetLogsDescription, prompts.GetLogsInstructions, attrCache.GetLogAttributes())
	getTracesDesc := buildEnhancedDescription(traces.GetTracesDescription, prompts.GetTracesInstructions, nil)
	getMetricsDesc := buildEnhancedDescription(apm.PromqlRangeQueryDetails, prompts.GetMetricsInstructions, nil)

	// Register exceptions tool
	last9mcp.RegisterInstrumentedTool(server, &mcp.Tool{
		Name:        "get_exceptions",
		Description: traces.GetExceptionsDescription,
	}, traces.NewGetExceptionsHandler(client, cfg))

	// Register service summary tool
	last9mcp.RegisterInstrumentedTool(server, &mcp.Tool{
		Name:        "get_service_summary",
		Description: apm.GetServiceSummaryDescription,
	}, apm.NewServiceSummaryHandler(client, cfg))

	// Register service environments tool
	last9mcp.RegisterInstrumentedTool(server, &mcp.Tool{
		Name:        "get_service_environments",
		Description: apm.GetServiceEnvironmentsDescription,
	}, apm.NewServiceEnvironmentsHandler(client, cfg))

	// Register service performance details tool
	last9mcp.RegisterInstrumentedTool(server, &mcp.Tool{
		Name:        "get_service_performance_details",
		Description: apm.GetServicePerformanceDetails,
	}, apm.NewServicePerformanceDetailsHandler(client, cfg))

	// Register service operations summary tool
	last9mcp.RegisterInstrumentedTool(server, &mcp.Tool{
		Name:        "get_service_operations_summary",
		Description: apm.GetServiceOperationsSummaryDescription,
	}, apm.NewServiceOperationsSummaryHandler(client, cfg))

	// Register service dependency graph tool
	last9mcp.RegisterInstrumentedTool(server, &mcp.Tool{
		Name:        "get_service_dependency_graph",
		Description: apm.GetServiceDependencyGraphDetails,
	}, apm.NewServiceDependencyGraphHandler(client, cfg))

	// Register PromQL range query tool (enhanced with metrics instructions)
	last9mcp.RegisterInstrumentedTool(server, &mcp.Tool{
		Name:        "prometheus_range_query",
		Description: getMetricsDesc,
	}, apm.NewPromqlRangeQueryHandler(client, cfg))

	// Register PromQL instant query tool
	last9mcp.RegisterInstrumentedTool(server, &mcp.Tool{
		Name:        "prometheus_instant_query",
		Description: apm.PromqlInstantQueryDetails,
	}, apm.NewPromqlInstantQueryHandler(client, cfg))

	// Register PromQL label values tool
	last9mcp.RegisterInstrumentedTool(server, &mcp.Tool{
		Name:        "prometheus_label_values",
		Description: apm.PromqlLabelValuesQueryDetails,
	}, apm.NewPromqlLabelValuesHandler(client, cfg))

	// Register PromQL labels tool
	last9mcp.RegisterInstrumentedTool(server, &mcp.Tool{
		Name:        "prometheus_labels",
		Description: apm.PromqlLabelsQueryDetails,
	}, apm.NewPromqlLabelsHandler(client, cfg))

	// Register logs tool (enhanced with log query instructions + labels)
	last9mcp.RegisterInstrumentedTool(server, &mcp.Tool{
		Name:        "get_logs",
		Description: getLogsDesc,
	}, logs.NewGetLogsHandler(client, cfg))

	// Register service logs tool
	last9mcp.RegisterInstrumentedTool(server, &mcp.Tool{
		Name:        "get_service_logs",
		Description: logs.GetServiceLogsDescription,
	}, logs.NewGetServiceLogsHandler(client, cfg))

	// Register drop rules tool
	last9mcp.RegisterInstrumentedTool(server, &mcp.Tool{
		Name:        "get_drop_rules",
		Description: logs.GetDropRulesDescription,
	}, logs.NewGetDropRulesHandler(client, cfg))

	// Register add drop rule tool
	last9mcp.RegisterInstrumentedTool(server, &mcp.Tool{
		Name:        "add_drop_rule",
		Description: logs.AddDropRuleDescription,
	}, logs.NewAddDropRuleHandler(client, cfg))

	// Register alert config tool
	last9mcp.RegisterInstrumentedTool(server, &mcp.Tool{
		Name:        "get_alert_config",
		Description: alerting.GetAlertConfigDescription,
	}, alerting.NewGetAlertConfigHandler(client, cfg))

	// Register alerts tool
	last9mcp.RegisterInstrumentedTool(server, &mcp.Tool{
		Name:        "get_alerts",
		Description: alerting.GetAlertsDescription,
	}, alerting.NewGetAlertsHandler(client, cfg))

	// Register get traces tool (enhanced with trace query instructions)
	last9mcp.RegisterInstrumentedTool(server, &mcp.Tool{
		Name:        "get_traces",
		Description: getTracesDesc,
	}, traces.NewGetTracesHandler(client, cfg))

	// Register service traces tool
	last9mcp.RegisterInstrumentedTool(server, &mcp.Tool{
		Name:        "get_service_traces",
		Description: traces.GetServiceTracesDescription,
	}, traces.GetServiceTracesHandler(client, cfg))

	// Register log attributes tool
	last9mcp.RegisterInstrumentedTool(server, &mcp.Tool{
		Name:        "get_log_attributes",
		Description: logs.GetLogAttributesDescription,
	}, logs.NewGetLogAttributesHandler(client, cfg))

	// Register trace attributes tool
	last9mcp.RegisterInstrumentedTool(server, &mcp.Tool{
		Name:        "get_trace_attributes",
		Description: traces.GetTraceAttributesDescription,
	}, traces.NewGetTraceAttributesHandler(client, cfg))

	// Register change events tool
	last9mcp.RegisterInstrumentedTool(server, &mcp.Tool{
		Name:        "get_change_events",
		Description: change_events.GetChangeEventsDescription,
	}, change_events.NewGetChangeEventsHandler(client, cfg))

	// Register system discovery tool
	last9mcp.RegisterInstrumentedTool(server, &mcp.Tool{
		Name:        "discover_system_components",
		Description: discovery.DiscoverSystemComponentsDescription,
	}, discovery.DiscoverSystemComponentsHandler(client, cfg))

	// Register metrics discovery tool
	last9mcp.RegisterInstrumentedTool(server, &mcp.Tool{
		Name:        "discover_metrics",
		Description: discovery.DiscoverMetricsDescription,
	}, discovery.DiscoverMetricsHandler(client, cfg))

	return nil
}
