package main

import (
	"context"
	"fmt"
	"last9-mcp/internal/alerting"
	"last9-mcp/internal/apm"
	"last9-mcp/internal/auth"
	"last9-mcp/internal/change_events"
	"last9-mcp/internal/discovery"
	"last9-mcp/internal/knowledge"
	"last9-mcp/internal/models"
	"last9-mcp/internal/telemetry/logs"
	"last9-mcp/internal/telemetry/traces"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	last9mcp "github.com/last9/mcp-go-sdk/mcp"
)

// registerAllTools registers all tools with the MCP server using the new SDK pattern.
// Returns the knowledge store so callers can pass it to prompt handlers.
func registerAllTools(server *last9mcp.Last9MCPServer, cfg models.Config) (knowledge.Store, error) {
	client := auth.GetHTTPClient()

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

	// Register get traces tool (JSON pipeline queries)
	last9mcp.RegisterInstrumentedTool(server, &mcp.Tool{
		Name:        "get_traces",
		Description: traces.GetTracesDescription,
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

	// --- Knowledge Graph Tools ---

	// Initialize Knowledge Store
	// Note: In a real app, this path might come from cfg or env
	kStore, err := knowledge.NewStore("")
	if err != nil {
		// Log error but don't fail startup, just disable KG tools?
		// For now, let's log and proceed, or fail if critical.
		// The prompt implies we want this feature, so let's error out if DB fails.
		return nil, fmt.Errorf("failed to init knowledge store: %w", err)
	}
	// Note: We are not closing kStore here because the server runs indefinitely.
	// Ideally, we'd pass this to main() to defer close, but refactoring main is separate.
	// SQLite close is optional on process exit.

	// Register builtin schemas (upserts definitions, preserves user service associations)
	if err := knowledge.RegisterBuiltinSchemas(context.Background(), kStore); err != nil {
		return nil, fmt.Errorf("failed to register builtin schemas: %w", err)
	}

	pipeline := knowledge.NewPipeline()

	// Register list_knowledge_schemas
	last9mcp.RegisterInstrumentedTool(server, &mcp.Tool{
		Name:        "list_knowledge_schemas",
		Description: knowledge.ListKnowledgeSchemasDescription,
	}, knowledge.NewListSchemasHandler(kStore))

	// Register add_service_to_schema
	last9mcp.RegisterInstrumentedTool(server, &mcp.Tool{
		Name:        "add_service_to_schema",
		Description: knowledge.AddServiceToSchemaDescription,
	}, knowledge.NewAddServiceToSchemaHandler(kStore))

	// Register remove_service_from_schema
	last9mcp.RegisterInstrumentedTool(server, &mcp.Tool{
		Name:        "remove_service_from_schema",
		Description: knowledge.RemoveServiceFromSchemaDescription,
	}, knowledge.NewRemoveServiceFromSchemaHandler(kStore))

	// Register define_knowledge_schema
	last9mcp.RegisterInstrumentedTool(server, &mcp.Tool{
		Name:        "define_knowledge_schema",
		Description: "Define a new architectural schema (pattern) that valid graphs should follow.",
	}, knowledge.NewDefineSchemaHandler(kStore))

	// Register ingest_knowledge
	last9mcp.RegisterInstrumentedTool(server, &mcp.Tool{
		Name: "ingest_knowledge",
		Description: `Ingest nodes, edges, statistics, and events into the knowledge graph.
Supports raw_text containing structured tool output (JSON from get_service_dependency_graph, discover_system_components, get_service_summary, get_service_operations_summary, prometheus queries) which is automatically parsed into nodes/edges/stats.
If the format is unrecognized or the internal parser fails, the tool returns an error instructing you to parse the text yourself and retry with structured JSON.`,
	}, knowledge.NewIngestHandler(kStore, pipeline))

	// Register search_knowledge_graph
	last9mcp.RegisterInstrumentedTool(server, &mcp.Tool{
		Name:        "search_knowledge_graph",
		Description: "Search for entities by keyword. Returns nodes, relationships, stats, recent events, and related notes. Note references include ID and title — use get_knowledge_note to retrieve full details.",
	}, knowledge.NewSearchHandler(kStore))

	// Register add_knowledge_note
	last9mcp.RegisterInstrumentedTool(server, &mcp.Tool{
		Name:        "add_knowledge_note",
		Description: knowledge.AddKnowledgeNoteDescription,
	}, knowledge.NewAddNoteHandler(kStore))

	// Register get_knowledge_note
	last9mcp.RegisterInstrumentedTool(server, &mcp.Tool{
		Name:        "get_knowledge_note",
		Description: knowledge.GetKnowledgeNoteDescription,
	}, knowledge.NewGetNoteHandler(kStore))

	// Register delete_knowledge_note
	last9mcp.RegisterInstrumentedTool(server, &mcp.Tool{
		Name:        "delete_knowledge_note",
		Description: knowledge.DeleteKnowledgeNoteDescription,
	}, knowledge.NewDeleteNoteHandler(kStore))

	return kStore, nil
}
