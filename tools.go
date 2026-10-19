package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"last9-mcp/internal/alerting"
	"last9-mcp/internal/apm"
	"last9-mcp/internal/auth"
	"last9-mcp/internal/change_events"
	"last9-mcp/internal/changeimpact"
	"last9-mcp/internal/dashboards"
	"last9-mcp/internal/grafana"
	"last9-mcp/internal/models"
	"last9-mcp/internal/prompts"
	"last9-mcp/internal/suggest"
	"last9-mcp/internal/telemetry/logs"
	"last9-mcp/internal/telemetry/profiles"
	"last9-mcp/internal/telemetry/traces"
	"last9-mcp/internal/toolsets"
	"last9-mcp/internal/utils"
	"last9-mcp/internal/workspace"

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

// toolGate applies the served-tool filter and records every tool offered to
// it, so --allow-write-tools names are checked against the real registry
// rather than a hand-kept list. Admitted tools register only in
// registerAdmitted, once the served set is final.
type toolGate struct {
	filter   toolsets.Filter
	readOnly map[string]bool // every offered tool name → its readOnlyHint
	served   toolsets.Set    // every admitted tool name
	pending  []func() error  // registrations of admitted tools, in offer order
}

func newToolGate(filter toolsets.Filter) *toolGate {
	return &toolGate{filter: filter, readOnly: make(map[string]bool), served: make(toolsets.Set)}
}

func (g *toolGate) admit(tool *mcp.Tool) bool {
	// Missing annotations count as write, so read-only mode fails closed.
	readOnly := tool.Annotations != nil && tool.Annotations.ReadOnlyHint
	g.readOnly[tool.Name] = readOnly
	if !g.filter.Allows(tool.Name, readOnly) {
		return false
	}
	g.served[tool.Name] = struct{}{}
	return true
}

// checkAllowWrite fails when an --allow-write-tools name is not a registered
// write tool, so a typo cannot silently leave a tool off. Call it after every
// tool has been offered.
func (g *toolGate) checkAllowWrite() error {
	var problems []string
	for name := range g.filter.AllowWrite {
		readOnly, known := g.readOnly[name]
		switch {
		case !known:
			problems = append(problems, fmt.Sprintf("unknown tool %q", name))
		case readOnly:
			problems = append(problems, fmt.Sprintf("%q is read-only and already served", name))
		}
	}
	if len(problems) == 0 {
		return nil
	}
	var writeNames []string
	for name, readOnly := range g.readOnly {
		if !readOnly {
			writeNames = append(writeNames, name)
		}
	}
	sort.Strings(problems)
	sort.Strings(writeNames)
	return fmt.Errorf("--allow-write-tools: %s; write tools: %s", strings.Join(problems, "; "), strings.Join(writeNames, ", "))
}

// registerAdmitted registers every admitted tool. It runs after every tool has
// been offered because description routes are trimmed to the served set, and
// a route may name a tool offered later or held back by read-only mode.
func (g *toolGate) registerAdmitted() error {
	pending := g.pending
	g.pending = nil
	for _, register := range pending {
		if err := register(); err != nil {
			return err
		}
	}
	return nil
}

// registerIfAllowed is the single registration gate: it queues a tool for
// registerAdmitted only when the gate's filter admits it (toolset, and
// read-only mode).
func registerIfAllowed[In, Out any](server *last9mcp.Last9MCPServer, gate *toolGate, tool *mcp.Tool, handler mcp.ToolHandlerFor[In, Out]) error {
	if !gate.admit(tool) {
		return nil
	}
	gate.pending = append(gate.pending, func() error {
		return registerTool(server, tool, handler, gate.served.Allows)
	})
	return nil
}

// registerTool registers one admitted tool, keeping only the description
// routes to tools served alongside it.
// The MCP SDK panics on invalid tool schemas; recover and return an error so
// registerAllTools can fail fast instead of crashing the process.
func registerTool[In, Out any](server *last9mcp.Last9MCPServer, tool *mcp.Tool, handler mcp.ToolHandlerFor[In, Out], served func(string) bool) (err error) {
	// Serve the annotation title as the tool's title too, so clients that read
	// either field show the same name.
	if tool.Title == "" && tool.Annotations != nil {
		tool.Title = tool.Annotations.Title
	}
	tool.Description = prompts.TrimRoutes(tool.Description, served)
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

// registerWriteTool registers a tool that changes Last9 state. verify is the read call
// that shows whether a write landed, starting with the read tool's name; the agent gets
// it whenever a failure leaves the outcome unknown, so a toolset serving the write tool
// must serve that read tool too.
func registerWriteTool[In, Out any](server *last9mcp.Last9MCPServer, gate *toolGate, tool *mcp.Tool, verify string, handler mcp.ToolHandlerFor[In, Out]) error {
	readTool := verifyToolName(verify)
	if readTool == "" {
		return fmt.Errorf("register %q: a write tool needs a verify call", tool.Name)
	}
	if gate.filter.Allows(tool.Name, false) && !gate.filter.Allows(readTool, true) {
		return fmt.Errorf("register %q: its verify tool %q is not in the same toolsets", tool.Name, readTool)
	}
	return registerIfAllowed(server, gate, tool, checkBeforeRetry(tool.Name, verify, handler))
}

// verifyToolName returns the read tool a verify call starts with.
func verifyToolName(verify string) string {
	verify = strings.TrimSpace(verify)
	end := strings.IndexFunc(verify, func(r rune) bool { return r != '_' && (r < 'a' || r > 'z') })
	if end < 0 {
		return verify
	}
	return verify[:end]
}

// checkBeforeRetry turns an unknown write outcome into a tool error that tells the agent
// how to check it, since retrying blindly can duplicate a create.
func checkBeforeRetry[In, Out any](name, verify string, handler mcp.ToolHandlerFor[In, Out]) mcp.ToolHandlerFor[In, Out] {
	return func(ctx context.Context, req *mcp.CallToolRequest, in In) (*mcp.CallToolResult, Out, error) {
		result, out, err := handler(ctx, req, in)
		var unknown *utils.WriteOutcomeUnknownError
		if errors.As(err, &unknown) {
			return utils.ToolErrorResult(fmt.Sprintf("%s may or may not have been applied (%s). Do not retry until you have checked: %s.",
				name, unknown.Cause, verify)), out, nil
		}
		return result, out, err
	}
}

// registerAllTools registers all tools with the MCP server using the new SDK
// pattern and returns the names it served.
func registerAllTools(server *last9mcp.Last9MCPServer, cfg models.Config) (toolsets.Set, error) {
	return registerTools(server, cfg, auth.GetHTTPClient())
}

func registerTools(server *last9mcp.Last9MCPServer, cfg models.Config, client *http.Client) (toolsets.Set, error) {
	gate := newToolGate(cfg.ToolFilter)
	var regErr error
	reg := func(err error) {
		if err != nil && regErr == nil {
			regErr = err
		}
	}

	reg(registerIfAllowed(server, gate, &mcp.Tool{
		Name:        "get_current_time",
		Annotations: readOnlyTool("Get Current Time"),
		Description: prompts.GetCurrentTimeDescription,
	}, workspace.NewGetCurrentTimeHandler(time.Now)))

	reg(registerIfAllowed(server, gate, &mcp.Tool{
		Name:        "get_workspace_overview",
		Annotations: readOnlyTool("Get Workspace Overview"),
		Description: prompts.GetWorkspaceOverviewDescription,
	}, workspace.NewGetWorkspaceOverviewHandler(client, cfg)))

	// Register exceptions tool
	reg(registerIfAllowed(server, gate, &mcp.Tool{
		Name:        "get_exceptions",
		Annotations: readOnlyTool("Get Exceptions"),
		Description: prompts.GetExceptionsInstructions,
	}, traces.NewGetExceptionsHandler(client, cfg)))

	// Register service summary tool
	reg(registerIfAllowed(server, gate, &mcp.Tool{
		Name:        "get_service_summary",
		Annotations: readOnlyTool("Get Service Summary"),
		Description: prompts.GetServiceSummaryDescription,
		InputSchema: apm.GetServiceSummaryInputSchema(),
	}, apm.NewServiceSummaryHandler(client, cfg)))

	// Register APM service deviations tool
	reg(registerIfAllowed(server, gate, &mcp.Tool{
		Name:        "get_apm_service_deviations",
		Annotations: readOnlyTool("Get APM Service Deviations"),
		Description: prompts.GetAPMServiceDeviationsDescription,
		InputSchema: apm.GetAPMServiceDeviationsInputSchema(),
	}, apm.NewAPMServiceDeviationsHandler(client, cfg)))

	// Register service environments tool
	reg(registerIfAllowed(server, gate, &mcp.Tool{
		Name:        "get_service_environments",
		Annotations: readOnlyTool("Get Service Environments"),
		Description: prompts.GetServiceEnvironmentsDescription,
	}, apm.NewServiceEnvironmentsHandler(client, cfg)))

	// Register service performance details tool
	reg(registerIfAllowed(server, gate, &mcp.Tool{
		Name:        "get_service_performance_details",
		Annotations: readOnlyTool("Get Service Performance Details"),
		Description: prompts.GetServicePerformanceDetails,
	}, apm.NewServicePerformanceDetailsHandler(client, cfg)))

	// Register service operations summary tool
	reg(registerIfAllowed(server, gate, &mcp.Tool{
		Name:        "get_service_operations_summary",
		Annotations: readOnlyTool("Get Service Operations Summary"),
		Description: prompts.GetServiceOperationsSummaryDescription,
	}, apm.NewServiceOperationsSummaryHandler(client, cfg)))

	// Register service dependency graph tool
	reg(registerIfAllowed(server, gate, &mcp.Tool{
		Name:        "get_service_dependency_graph",
		Annotations: readOnlyTool("Get Service Dependency Graph"),
		Description: prompts.GetServiceDependencyGraphDetails,
	}, apm.NewServiceDependencyGraphHandler(client, cfg)))

	// Register list datasources tool
	reg(registerIfAllowed(server, gate, &mcp.Tool{
		Name:        "list_datasources",
		Annotations: readOnlyTool("List Datasources"),
		Description: prompts.ListDatasourcesDescription,
	}, apm.NewListDatasourcesHandler(cfg)))

	// Register PromQL range query tool (enhanced with metrics instructions)
	reg(registerIfAllowed(server, gate, &mcp.Tool{
		Name:        "prometheus_range_query",
		Annotations: readOnlyTool("Run PromQL Range Query"),
		Description: prompts.PromqlRangeQueryDetails,
		InputSchema: apm.PromqlRangeQueryInputSchema(),
	}, apm.NewPromqlRangeQueryHandler(client, cfg)))

	// Register PromQL instant query tool
	reg(registerIfAllowed(server, gate, &mcp.Tool{
		Name:        "prometheus_instant_query",
		Annotations: readOnlyTool("Run PromQL Instant Query"),
		Description: prompts.PromqlInstantQueryDetails,
		InputSchema: apm.PromqlInstantQueryInputSchema(),
	}, apm.NewPromqlInstantQueryHandler(client, cfg)))

	// Register PromQL label values tool
	reg(registerIfAllowed(server, gate, &mcp.Tool{
		Name:        "prometheus_label_values",
		Annotations: readOnlyTool("Get Prometheus Label Values"),
		Description: prompts.PromqlLabelValuesQueryDetails,
	}, apm.NewPromqlLabelValuesHandler(client, cfg)))

	// Register PromQL labels tool
	reg(registerIfAllowed(server, gate, &mcp.Tool{
		Name:        "prometheus_labels",
		Annotations: readOnlyTool("Get Prometheus Labels"),
		Description: prompts.PromqlLabelsQueryDetails,
	}, apm.NewPromqlLabelsHandler(client, cfg)))

	// Register logs tool (enhanced with log query instructions + labels)
	reg(registerIfAllowed(server, gate, &mcp.Tool{
		Name:        "get_logs",
		Annotations: readOnlyTool("Get Logs"),
		Description: prompts.GetLogsDescription,
		InputSchema: logs.GetLogsInputSchema(),
	}, logs.NewGetLogsHandler(client, cfg)))

	// Register service logs tool
	reg(registerIfAllowed(server, gate, &mcp.Tool{
		Name:        "get_service_logs",
		Meta:        mcp.Meta{"last9/coverage": true},
		Annotations: readOnlyTool("Get Service Logs"),
		Description: prompts.GetServiceLogsDescription,
		InputSchema: logs.GetServiceLogsInputSchema(),
	}, logs.NewGetServiceLogsHandler(client, cfg)))

	reg(registerIfAllowed(server, gate, &mcp.Tool{
		Name:        "get_log_patterns",
		Annotations: readOnlyTool("Get Log Patterns"),
		Description: prompts.GetLogPatternsDescription,
		InputSchema: logs.GetLogPatternsInputSchema(),
	}, logs.NewGetLogPatternsHandler(client, cfg)))

	reg(registerIfAllowed(server, gate, &mcp.Tool{
		Name:        "get_emerging_log_patterns",
		Annotations: readOnlyTool("Get Emerging Log Patterns"),
		Description: prompts.GetEmergingLogPatternsDescription,
		InputSchema: logs.GetEmergingLogPatternsInputSchema(),
	}, logs.NewGetEmergingLogPatternsHandler(client, cfg)))

	// Register drop rules tool
	reg(registerIfAllowed(server, gate, &mcp.Tool{
		Name:        "get_drop_rules",
		Annotations: readOnlyTool("Get Log Drop Rules"),
		Description: prompts.GetDropRulesDescription,
	}, logs.NewGetDropRulesHandler(client, cfg)))

	// Register add drop rule tool
	reg(registerWriteTool(server, gate, &mcp.Tool{
		Name:        "add_drop_rule",
		Annotations: writeTool("Add Log Drop Rule", true, false),
		Description: prompts.AddDropRuleDescription,
	}, "get_drop_rules, looking for a rule with the name you passed",
		logs.NewAddDropRuleHandler(client, cfg)))

	// Register notification channels tool
	reg(registerIfAllowed(server, gate, &mcp.Tool{
		Name:        "get_notification_channels",
		Annotations: readOnlyTool("Get Notification Channels"),
		Description: prompts.GetNotificationChannelsDescription,
	}, alerting.NewGetNotificationChannelsHandler(client, cfg)))

	// Register alert config tool
	reg(registerIfAllowed(server, gate, &mcp.Tool{
		Name:        "get_alert_config",
		Annotations: readOnlyTool("Get Alert Config"),
		Description: prompts.GetAlertConfigDescription,
	}, alerting.NewGetAlertConfigHandler(client, cfg)))

	reg(registerWriteTool(server, gate, &mcp.Tool{
		Name:        "create_alert",
		Annotations: writeTool("Create Alert", false, false),
		Description: prompts.CreateAlertDescription,
	}, "get_alert_config with rule_name set to the rule_name you passed (if you passed none, get_entity_alert_rules for entity_id)",
		alerting.NewCreateAlertHandler(client, cfg)))

	reg(registerWriteTool(server, gate, &mcp.Tool{
		Name:        "update_alert",
		Annotations: writeTool("Update Alert", true, true),
		Description: prompts.UpdateAlertDescription,
	}, "get_entity_alert_rules for entity_id, comparing the rule to the alert_rule you sent (an update may replace the rule id)",
		alerting.NewUpdateAlertHandler(client, cfg)))

	reg(registerWriteTool(server, gate, &mcp.Tool{
		Name:        "patch_alert",
		Annotations: writeTool("Patch Alert", true, true),
		Description: prompts.PatchAlertDescription,
	}, "get_entity_alert_rules for entity_id, checking the fields you changed on rule id",
		alerting.NewPatchAlertHandler(client, cfg)))

	reg(registerWriteTool(server, gate, &mcp.Tool{
		Name:        "delete_alert",
		Annotations: writeTool("Delete Alert", true, true),
		Description: prompts.DeleteAlertDescription,
	}, "get_entity_alert_rules for entity_id; rule id is no longer listed if the delete landed",
		alerting.NewDeleteAlertHandler(client, cfg)))

	reg(registerWriteTool(server, gate, &mcp.Tool{
		Name:        "mute_alert_group",
		Annotations: writeTool("Mute Alert Group", true, true),
		Description: prompts.MuteAlertGroupDescription,
	}, "get_entity_alert_rules for entity_id, checking mute_until on its rules",
		alerting.NewMuteAlertGroupHandler(client, cfg)))

	reg(registerWriteTool(server, gate, &mcp.Tool{
		Name:        "acknowledge_alert",
		Annotations: writeTool("Acknowledge Alert", false, true),
		Description: prompts.AcknowledgeAlertDescription,
	}, "get_alerts, checking the claim on this alert",
		alerting.NewAcknowledgeAlertHandler(client, cfg)))

	reg(registerWriteTool(server, gate, &mcp.Tool{
		Name:        "reassign_alert",
		Annotations: writeTool("Reassign Alert", false, true),
		Description: prompts.ReassignAlertDescription,
	}, "get_alerts, checking the claim owner on this alert",
		alerting.NewReassignAlertHandler(client, cfg)))

	reg(registerWriteTool(server, gate, &mcp.Tool{
		Name:        "create_notification_channel",
		Annotations: writeTool("Create Notification Channel", false, false),
		Description: prompts.CreateNotificationChannelDescription,
	}, "get_notification_channels with scope=channels, looking for a channel with the name you passed",
		alerting.NewCreateNotificationChannelHandler(client, cfg)))

	reg(registerWriteTool(server, gate, &mcp.Tool{
		Name:        "attach_notification_channel",
		Annotations: writeTool("Attach Notification Channel", false, true),
		Description: prompts.AttachNotificationChannelDescription,
	}, "get_notification_channels with scope=attached, looking for channel_id on alert_group_id",
		alerting.NewAttachNotificationChannelHandler(client, cfg)))

	reg(registerWriteTool(server, gate, &mcp.Tool{
		Name:        "detach_notification_channel",
		Annotations: writeTool("Detach Notification Channel", true, true),
		Description: prompts.DetachNotificationChannelDescription,
	}, "get_notification_channels with scope=attached; attachment_id is no longer listed if the detach landed",
		alerting.NewDetachNotificationChannelHandler(client, cfg)))

	reg(registerWriteTool(server, gate, &mcp.Tool{
		Name:        "delete_notification_channel",
		Annotations: writeTool("Delete Notification Channel", true, false),
		Description: prompts.DeleteNotificationChannelDescription,
	}, "get_notification_channels with scope=channels; channel_id is no longer listed if the delete landed",
		alerting.NewDeleteNotificationChannelHandler(client, cfg)))

	reg(registerIfAllowed(server, gate, &mcp.Tool{
		Name:        "validate_alert_expression",
		Annotations: readOnlyTool("Validate Alert Expression"),
		Description: prompts.ValidateAlertExpressionDescription,
	}, alerting.NewValidateAlertExpressionHandler(client, cfg)))

	reg(registerIfAllowed(server, gate, &mcp.Tool{
		Name:        "recommend_alert_config",
		Annotations: readOnlyTool("Recommend Alert Config"),
		Description: prompts.RecommendAlertConfigDescription,
	}, alerting.NewRecommendAlertConfigHandler(client, cfg)))

	reg(registerIfAllowed(server, gate, &mcp.Tool{
		Name:        "get_alert_groups",
		Annotations: readOnlyTool("Get Alert Groups"),
		Description: prompts.GetAlertGroupsDescription,
	}, alerting.NewGetAlertGroupsHandler(client, cfg)))

	// Register entity alert rules tool (entity-scoped, includes expression_args and resolved PromQL)
	reg(registerIfAllowed(server, gate, &mcp.Tool{
		Name:        "get_entity_alert_rules",
		Annotations: readOnlyTool("Get Entity Alert Rules"),
		Description: prompts.GetEntityAlertRulesDescription,
	}, alerting.NewGetEntityAlertRulesHandler(client, cfg)))

	reg(registerIfAllowed(server, gate, &mcp.Tool{
		Name:        "get_entity_kpis",
		Annotations: readOnlyTool("Get Entity KPIs"),
		Description: prompts.GetEntityKPIsDescription,
	}, alerting.NewGetEntityKPIsHandler(client, cfg)))

	reg(registerWriteTool(server, gate, &mcp.Tool{
		Name:        "create_kpi",
		Annotations: writeTool("Create KPI", false, false),
		Description: prompts.CreateKPIDescription,
	}, "get_entity_kpis for entity_id, looking for a KPI with the name you passed",
		alerting.NewCreateKPIHandler(client, cfg)))

	// Register describe alert chart tool (read-only enumerate of alertable signals on a covered Discover chart)
	reg(registerIfAllowed(server, gate, &mcp.Tool{
		Name:        "describe_alert_chart",
		Annotations: readOnlyTool("Describe Alert Chart"),
		Description: prompts.DescribeAlertChartDescription,
	}, alerting.NewDescribeAlertChartHandler(client, cfg)))

	// Register create alert from chart tool (one-call static-threshold rule from chart identity)
	reg(registerWriteTool(server, gate, &mcp.Tool{
		Name:        "create_alert_from_chart",
		Annotations: writeTool("Create Alert From Chart", false, false),
		Description: prompts.CreateAlertFromChartDescription,
	}, "get_alert_config with rule_name set to the name you passed",
		alerting.NewCreateAlertFromChartHandler(client, cfg)))

	// Register alerts tool
	reg(registerIfAllowed(server, gate, &mcp.Tool{
		Name:        "get_alerts",
		Annotations: readOnlyTool("Get Alerts"),
		Description: prompts.GetAlertsDescription,
	}, alerting.NewGetAlertsHandler(client, cfg)))

	// Register get alert rule state tool
	reg(registerIfAllowed(server, gate, &mcp.Tool{
		Name:        "get_alert_rule_state",
		Annotations: readOnlyTool("Get Alert Rule State"),
		Description: prompts.GetAlertRuleStateDescription,
	}, alerting.NewAlertRuleStateHandler(client, cfg)))

	// Register get traces tool (enhanced with trace query instructions)
	reg(registerIfAllowed(server, gate, &mcp.Tool{
		Name:        "get_traces",
		Annotations: readOnlyTool("Get Traces"),
		Description: prompts.GetTracesDescription,
		InputSchema: traces.GetTracesInputSchema(),
	}, traces.NewGetTracesHandler(client, cfg)))

	// Register service traces tool
	reg(registerIfAllowed(server, gate, &mcp.Tool{
		Name:        "get_service_traces",
		Annotations: readOnlyTool("Get Service Traces"),
		Description: prompts.GetServiceTracesDescription,
		InputSchema: traces.GetServiceTracesInputSchema(),
	}, traces.GetServiceTracesHandler(client, cfg)))

	// Register log attributes tool
	reg(registerIfAllowed(server, gate, &mcp.Tool{
		Name:        "get_log_attributes",
		Annotations: readOnlyTool("Get Log Attributes"),
		Description: prompts.GetLogAttributesDescription,
	}, logs.NewGetLogAttributesHandler(client, cfg)))

	// Register pipeline-scoped log attributes tool (discovers fields actually
	// present for a given pipeline via the series endpoint)
	reg(registerIfAllowed(server, gate, &mcp.Tool{
		Name:        "get_log_attributes_for_pipeline",
		Annotations: readOnlyTool("Get Log Attributes for Pipeline"),
		Description: prompts.GetLogAttributesForPipelineDescription,
	}, logs.NewGetLogAttributesForPipelineHandler(client, cfg)))

	// Register trace attributes tool
	reg(registerIfAllowed(server, gate, &mcp.Tool{
		Name:        "get_trace_attributes",
		Annotations: readOnlyTool("Get Trace Attributes"),
		Description: prompts.GetTraceAttributesDescription,
	}, traces.NewGetTraceAttributesHandler(client, cfg)))

	// Register pipeline-scoped trace attributes tool (discovers attributes actually
	// present for a given pipeline via the series endpoint)
	reg(registerIfAllowed(server, gate, &mcp.Tool{
		Name:        "get_trace_attributes_for_pipeline",
		Annotations: readOnlyTool("Get Trace Attributes for Pipeline"),
		Description: prompts.GetTraceAttributesForPipelineDescription,
	}, traces.NewGetTraceAttributesForPipelineHandler(client, cfg)))

	// Register trace attribute values tool
	reg(registerIfAllowed(server, gate, &mcp.Tool{
		Name:        "get_trace_attribute_values",
		Annotations: readOnlyTool("Get Trace Attribute Values"),
		Description: prompts.GetTraceAttributeValuesDescription,
	}, traces.NewGetTraceAttributeValuesHandler(client, cfg)))

	// Register bounded cohort attribute deviation tool
	reg(registerIfAllowed(server, gate, &mcp.Tool{
		Name:        "get_trace_attribute_deviations",
		Annotations: readOnlyTool("Get Trace Attribute Deviations"),
		Description: prompts.GetTraceAttributeDeviationsDescription,
	}, traces.NewGetTraceAttributeDeviationsHandler(client, cfg)))

	// Register exact-trace waterfall tool
	reg(registerIfAllowed(server, gate, &mcp.Tool{
		Name:        "get_trace_waterfall",
		Annotations: readOnlyTool("Get Trace Waterfall"),
		Description: prompts.GetTraceWaterfallDescription,
		InputSchema: traces.GetTraceWaterfallInputSchema(),
	}, traces.NewGetTraceWaterfallHandler(client, cfg)))

	// Register change events tool
	reg(registerIfAllowed(server, gate, &mcp.Tool{
		Name:        "get_change_events",
		Annotations: readOnlyTool("Get Change Events"),
		Description: prompts.GetChangeEventsDescription,
	}, change_events.NewGetChangeEventsHandler(client, cfg)))

	reg(registerIfAllowed(server, gate, &mcp.Tool{
		Name:        "analyze_change_impact",
		Annotations: readOnlyTool("Analyze Change Impact"),
		Description: prompts.AnalyzeChangeImpactDescription,
	}, changeimpact.NewAnalyzeChangeImpactHandler(client, cfg)))

	// Register database discovery tool
	reg(registerIfAllowed(server, gate, &mcp.Tool{
		Name:        "get_databases",
		Annotations: readOnlyTool("Get Databases"),
		Description: prompts.GetDatabasesDescription,
	}, apm.NewGetDatabasesHandler(client, cfg)))

	// Register database slow queries tool
	reg(registerIfAllowed(server, gate, &mcp.Tool{
		Name:        "get_database_slow_queries",
		Annotations: readOnlyTool("Get Database Slow Queries"),
		Description: prompts.GetDatabaseSlowQueriesDescription,
	}, apm.NewGetDatabaseSlowQueriesHandler(client, cfg)))

	// Register database query patterns tool
	reg(registerIfAllowed(server, gate, &mcp.Tool{
		Name:        "get_database_queries",
		Annotations: readOnlyTool("Get Database Queries"),
		Description: prompts.GetDatabaseQueriesDescription,
	}, apm.NewGetDatabaseQueriesHandler(client, cfg)))

	// Register database server-side metrics tool
	reg(registerIfAllowed(server, gate, &mcp.Tool{
		Name:        "get_database_server_metrics",
		Annotations: readOnlyTool("Get Database Server Metrics"),
		Description: prompts.GetDatabaseServerMetricsDescription,
	}, apm.NewGetDatabaseServerMetricsHandler(client, cfg)))

	// Register did_you_mean tool
	reg(registerIfAllowed(server, gate, &mcp.Tool{
		Name:        "did_you_mean",
		Annotations: readOnlyTool("Suggest Matching Names"),
		Description: prompts.DidYouMeanDescription,
	}, suggest.NewDidYouMeanHandler(client, cfg)))

	// Register service profile tool
	reg(registerIfAllowed(server, gate, &mcp.Tool{
		Name:        "get_service_profile",
		Annotations: readOnlyTool("Get Service Profile"),
		Description: prompts.GetServiceProfileDescription,
	}, apm.NewGetServiceProfileHandler(client, cfg)))

	// Continuous profiling tools (query_range/json)
	reg(registerIfAllowed(server, gate, &mcp.Tool{
		Name:        "get_profile_services",
		Annotations: readOnlyTool("Get Profiled Services"),
		Description: prompts.GetProfileServicesDescription,
	}, profiles.NewGetProfileServicesHandler(client, cfg)))

	reg(registerIfAllowed(server, gate, &mcp.Tool{
		Name:        "get_flamegraph",
		Annotations: readOnlyTool("Get Flame Graph"),
		Description: prompts.GetFlamegraphDescription,
	}, profiles.NewGetFlamegraphHandler(client, cfg)))

	reg(registerIfAllowed(server, gate, &mcp.Tool{
		Name:        "get_top_functions",
		Annotations: readOnlyTool("Get Top Functions"),
		Description: prompts.GetTopFunctionsDescription,
	}, profiles.NewGetTopFunctionsHandler(client, cfg)))

	reg(registerIfAllowed(server, gate, &mcp.Tool{
		Name:        "get_profile_summary",
		Annotations: readOnlyTool("Get Profile Summary"),
		Description: prompts.GetProfileSummaryDescription,
	}, profiles.NewGetProfileSummaryHandler(client, cfg)))

	// Register dashboard tools
	reg(registerIfAllowed(server, gate, &mcp.Tool{
		Name:        "list_dashboards",
		Annotations: readOnlyTool("List Dashboards"),
		Description: prompts.ListDashboardsDescription,
	}, dashboards.NewListDashboardsHandler(client, cfg)))

	reg(registerIfAllowed(server, gate, &mcp.Tool{
		Name:        "get_dashboard",
		Annotations: readOnlyTool("Get Dashboard"),
		Description: prompts.GetDashboardDescription,
	}, dashboards.NewGetDashboardHandler(client, cfg)))

	reg(registerIfAllowed(server, gate, &mcp.Tool{
		Name:        "validate_dashboard",
		Annotations: readOnlyTool("Validate Dashboard"),
		Description: prompts.ValidateDashboardDescription,
		InputSchema: dashboards.GetValidateDashboardInputSchema(),
	}, dashboards.NewValidateDashboardHandler(client, cfg)))

	reg(registerWriteTool(server, gate, &mcp.Tool{
		Name:        "create_dashboard",
		Annotations: writeTool("Create Dashboard", false, false),
		Description: prompts.CreateDashboardDescription,
		InputSchema: dashboards.GetCreateDashboardInputSchema(),
	}, "list_dashboards, looking for a dashboard with the name you passed",
		dashboards.NewCreateDashboardHandler(client, cfg)))

	reg(registerWriteTool(server, gate, &mcp.Tool{
		Name:        "update_dashboard",
		Annotations: writeTool("Update Dashboard", true, true),
		Description: prompts.UpdateDashboardDescription,
		InputSchema: dashboards.GetUpdateDashboardInputSchema(),
	}, "get_dashboard for id, comparing it to the dashboard you sent",
		dashboards.NewUpdateDashboardHandler(client, cfg)))

	reg(registerWriteTool(server, gate, &mcp.Tool{
		Name:        "delete_dashboard",
		Annotations: writeTool("Delete Dashboard", true, true),
		Description: prompts.DeleteDashboardDescription,
	}, "list_dashboards; id is no longer listed if the delete landed",
		dashboards.NewDeleteDashboardHandler(client, cfg)))

	reg(registerIfAllowed(server, gate, &mcp.Tool{
		Name:        "list_dashboard_snapshots",
		Annotations: readOnlyTool("List Dashboard Snapshots"),
		Description: prompts.ListDashboardSnapshotsDescription,
	}, dashboards.NewListDashboardSnapshotsHandler(client, cfg)))

	reg(registerIfAllowed(server, gate, &mcp.Tool{
		Name:        "get_dashboard_snapshot",
		Annotations: readOnlyTool("Get Dashboard Snapshot"),
		Description: prompts.GetDashboardSnapshotDescription,
	}, dashboards.NewGetDashboardSnapshotHandler(client, cfg)))

	reg(registerWriteTool(server, gate, &mcp.Tool{
		Name:        "delete_dashboard_snapshot",
		Annotations: writeTool("Delete Dashboard Snapshot", true, true),
		Description: prompts.DeleteDashboardSnapshotDescription,
	}, "get_dashboard_snapshot for id; it reports not found if the delete landed",
		dashboards.NewDeleteDashboardSnapshotHandler(client, cfg)))

	// Register Grafana read tools
	reg(registerIfAllowed(server, gate, &mcp.Tool{
		Name:        "grafana_search_dashboards",
		Annotations: readOnlyTool("Search Grafana Dashboards"),
		Description: prompts.GrafanaSearchDashboardsDescription,
	}, grafana.NewSearchDashboardsHandler(client, cfg)))

	reg(registerIfAllowed(server, gate, &mcp.Tool{
		Name:        "grafana_get_dashboard",
		Annotations: readOnlyTool("Get Grafana Dashboard"),
		Description: prompts.GrafanaGetDashboardDescription,
	}, grafana.NewGetDashboardHandler(client, cfg)))

	reg(registerIfAllowed(server, gate, &mcp.Tool{
		Name:        "grafana_list_folders",
		Annotations: readOnlyTool("List Grafana Folders"),
		Description: prompts.GrafanaListFoldersDescription,
	}, grafana.NewListFoldersHandler(client, cfg)))

	reg(registerIfAllowed(server, gate, &mcp.Tool{
		Name:        "grafana_list_folder_dashboards",
		Annotations: readOnlyTool("List Grafana Folder Dashboards"),
		Description: prompts.GrafanaListFolderDashboardsDescription,
	}, grafana.NewListFolderDashboardsHandler(client, cfg)))

	reg(registerIfAllowed(server, gate, &mcp.Tool{
		Name:        "grafana_list_datasources",
		Annotations: readOnlyTool("List Grafana Datasources"),
		Description: prompts.GrafanaListDatasourcesDescription,
	}, grafana.NewListDatasourcesHandler(client, cfg)))

	reg(gate.checkAllowWrite())
	if regErr != nil {
		return nil, regErr
	}
	return gate.served, gate.registerAdmitted()
}
