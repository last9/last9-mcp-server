package main

import (
	"net/http"
	"time"

	"last9-mcp/internal/alerting"
	"last9-mcp/internal/apm"
	"last9-mcp/internal/change_events"
	"last9-mcp/internal/models"
	"last9-mcp/internal/telemetry/logs"
	"last9-mcp/internal/telemetry/traces"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	last9mcp "github.com/last9/mcp-go-sdk/mcp"
)

// registerAllTools registers all tools with the MCP server using the new SDK pattern
func registerAllTools(server *last9mcp.Last9MCPServer, cfg models.Config) error {
	client := last9mcp.WithHTTPTracing(&http.Client{
		Timeout: 30 * time.Second,
	})

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

	// Register PromQL range query tool
	last9mcp.RegisterInstrumentedTool(server, &mcp.Tool{
		Name:        "prometheus_range_query",
		Description: apm.PromqlRangeQueryDetails,
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

	// Register logs tool
	last9mcp.RegisterInstrumentedTool(server, &mcp.Tool{
		Name:        "get_logs",
		Description: logs.GetLogsDescription,
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

	// Register get traces tool
	last9mcp.RegisterInstrumentedTool(server, &mcp.Tool{
		Name:        "get_traces",
		Description: traces.GetTracesDescription,
	}, traces.GetTracesHandler(client, cfg))

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

	// Register general traces tool
	last9mcp.RegisterInstrumentedTool(server, &mcp.Tool{
		Name:        "get_traces",
		Description: traces.GetTracesDescription,
	}, traces.NewGetTracesHandler(client, cfg))

	return nil
}
