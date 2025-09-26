package main

import (
	"net/http"
	"time"

	"last9-mcp/internal/alerting"
	"last9-mcp/internal/apm"
	"last9-mcp/internal/models"
	"last9-mcp/internal/telemetry/logs"
	"last9-mcp/internal/telemetry/metrics"
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

	// Register service graph tool
	last9mcp.RegisterInstrumentedTool(server, &mcp.Tool{
		Name:        "get_service_graph",
		Description: traces.GetServiceGraphDescription,
	}, traces.NewGetServiceGraphHandler(client, cfg))

	// Register service traces tool
	last9mcp.RegisterInstrumentedTool(server, &mcp.Tool{
		Name:        "get_service_traces",
		Description: traces.GetServiceTracesDescription,
	}, traces.GetServiceTracesHandler(client, cfg))

	// Register service metrics tool
	last9mcp.RegisterInstrumentedTool(server, &mcp.Tool{
		Name:        "get_service_metrics",
		Description: metrics.GetServiceMetricsDescription,
	}, metrics.GetServiceMetricsHandler(client, cfg))

	// Register service operations summary tool
	last9mcp.RegisterInstrumentedTool(server, &mcp.Tool{
		Name:        "get_service_operations_summary",
		Description: apm.GetServiceOperationsSummaryDescription,
	}, apm.NewServiceOperationsSummaryHandler(client, cfg))

	// Register service dependency graph tool
	last9mcp.RegisterInstrumentedTool(server, &mcp.Tool{
		Name:        "get_service_dependency_graph",
		Description: apm.GetServiceDependencyGraphDescription,
	}, apm.NewServiceDependencyGraphHandler(client, cfg))

	// Register PromQL range query tool
	last9mcp.RegisterInstrumentedTool(server, &mcp.Tool{
		Name:        "promql_range_query",
		Description: apm.GetPromqlRangeQueryDescription,
	}, apm.NewPromqlRangeQueryHandler(client, cfg))

	// Register PromQL instant query tool
	last9mcp.RegisterInstrumentedTool(server, &mcp.Tool{
		Name:        "promql_instant_query",
		Description: apm.GetPromqlInstantQueryDescription,
	}, apm.NewPromqlInstantQueryHandler(client, cfg))

	// Register service environments tool
	last9mcp.RegisterInstrumentedTool(server, &mcp.Tool{
		Name:        "get_service_environments",
		Description: apm.GetServiceEnvironmentsDescription,
	}, apm.NewServiceEnvironmentsHandler(client, cfg))

	// Register PromQL label values tool
	last9mcp.RegisterInstrumentedTool(server, &mcp.Tool{
		Name:        "promql_label_values",
		Description: apm.GetPromqlLabelValuesDescription,
	}, apm.NewPromqlLabelValuesHandler(client, cfg))

	// Register PromQL labels tool
	last9mcp.RegisterInstrumentedTool(server, &mcp.Tool{
		Name:        "promql_labels",
		Description: apm.GetPromqlLabelsDescription,
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

	// Register alerts tools
	last9mcp.RegisterInstrumentedTool(server, &mcp.Tool{
		Name:        "get_alerts",
		Description: alerting.GetAlertsDescription,
	}, alerting.NewGetAlertsHandler(client, cfg))

	return nil
}
