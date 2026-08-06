package prompts

import _ "embed"

//go:embed descriptions/get_exceptions.md
var GetExceptionsInstructions string

//go:embed references/logjson.md
var LogjsonReference string

//go:embed references/tracejson.md
var TracejsonReference string

//go:embed references/service_logs.md
var ServiceLogsReference string

//go:embed references/metrics.md
var MetricsReference string

//go:embed descriptions/get_service_summary.md
var GetServiceSummaryDescription string

//go:embed descriptions/get_apm_service_deviations.md
var GetAPMServiceDeviationsDescription string

//go:embed descriptions/get_service_environments.md
var GetServiceEnvironmentsDescription string

//go:embed descriptions/get_service_performance_details.md
var GetServicePerformanceDetails string

//go:embed descriptions/get_service_operations_summary.md
var GetServiceOperationsSummaryDescription string

//go:embed descriptions/get_service_dependency_graph.md
var GetServiceDependencyGraphDetails string

//go:embed descriptions/list_datasources.md
var ListDatasourcesDescription string

//go:embed descriptions/prometheus_instant_query.md
var PromqlInstantQueryDetails string

//go:embed descriptions/prometheus_label_values.md
var PromqlLabelValuesQueryDetails string

//go:embed descriptions/prometheus_labels.md
var PromqlLabelsQueryDetails string

//go:embed descriptions/get_drop_rules.md
var GetDropRulesDescription string

//go:embed descriptions/add_drop_rule.md
var AddDropRuleDescription string

//go:embed descriptions/get_notification_channels.md
var GetNotificationChannelsDescription string

//go:embed descriptions/get_alert_config.md
var GetAlertConfigDescription string

//go:embed descriptions/get_entity_alert_rules.md
var GetEntityAlertRulesDescription string

//go:embed descriptions/get_alerts.md
var GetAlertsDescription string

//go:embed descriptions/get_alert_rule_state.md
var GetAlertRuleStateDescription string

//go:embed descriptions/get_log_attributes.md
var GetLogAttributesDescription string

//go:embed descriptions/get_log_attributes_for_pipeline.md
var GetLogAttributesForPipelineDescription string

//go:embed descriptions/get_trace_attributes.md
var GetTraceAttributesDescription string

//go:embed descriptions/get_trace_attribute_values.md
var GetTraceAttributeValuesDescription string

//go:embed descriptions/get_trace_attributes_for_pipeline.md
var GetTraceAttributesForPipelineDescription string

//go:embed descriptions/get_trace_attribute_deviations.md
var GetTraceAttributeDeviationsDescription string

//go:embed descriptions/get_trace_waterfall.md
var GetTraceWaterfallDescription string

//go:embed descriptions/get_change_events.md
var GetChangeEventsDescription string

//go:embed descriptions/get_databases.md
var GetDatabasesDescription string

//go:embed descriptions/get_database_slow_queries.md
var GetDatabaseSlowQueriesDescription string

//go:embed descriptions/get_database_queries.md
var GetDatabaseQueriesDescription string

//go:embed descriptions/get_database_server_metrics.md
var GetDatabaseServerMetricsDescription string

//go:embed descriptions/did_you_mean.md
var DidYouMeanDescription string

//go:embed descriptions/list_dashboards.md
var ListDashboardsDescription string

//go:embed descriptions/get_dashboard.md
var GetDashboardDescription string

//go:embed descriptions/create_dashboard.md
var CreateDashboardDescription string

//go:embed descriptions/update_dashboard.md
var UpdateDashboardDescription string

//go:embed descriptions/delete_dashboard.md
var DeleteDashboardDescription string

//go:embed descriptions/list_dashboard_snapshots.md
var ListDashboardSnapshotsDescription string

//go:embed descriptions/get_dashboard_snapshot.md
var GetDashboardSnapshotDescription string

//go:embed descriptions/delete_dashboard_snapshot.md
var DeleteDashboardSnapshotDescription string

//go:embed descriptions/pulse_subscriptions.md
var PulseSubscriptionsDescription string

//go:embed descriptions/pulse_reports.md
var PulseReportsDescription string

//go:embed descriptions/pulse_dispositions.md
var PulseDispositionsDescription string

//go:embed descriptions/get_logs_base.md
var GetLogsDescription string

//go:embed descriptions/get_service_logs_base.md
var GetServiceLogsDescription string

//go:embed descriptions/get_traces_base.md
var GetTracesDescription string

//go:embed descriptions/get_service_traces_base.md
var GetServiceTracesDescription string

//go:embed descriptions/prometheus_range_query_base.md
var PromqlRangeQueryDetails string

//go:embed workflows/scoped_log_attribute_discovery.md
var ScopedLogAttributeDiscoveryWorkflow string

//go:embed workflows/exception_root_cause_investigation.md
var ExceptionRootCauseInvestigationWorkflow string

//go:embed workflows/investigate_latency_spike.md
var InvestigateLatencySpikeWorkflow string

//go:embed workflows/diagnose_error_rate.md
var DiagnoseErrorRateWorkflow string

//go:embed workflows/analyze_slow_queries.md
var AnalyzeSlowQueriesWorkflow string

//go:embed workflows/on_call_runbook.md
var OnCallRunbookWorkflow string
