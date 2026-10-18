{{- $services := tools "did_you_mean" "get_service_environments" "get_profile_services" "list_datasources" "grafana_list_datasources" -}}
{{- $attributes := tools "get_log_attributes" "get_log_attributes_for_pipeline" "get_trace_attributes" "get_trace_attributes_for_pipeline" "get_trace_attribute_values" -}}
{{- $metricNames := tools "prometheus_labels" "prometheus_label_values" -}}
{{- $alertRules := tools "get_alert_groups" "get_alert_config" "get_entity_alert_rules" "get_notification_channels" -}}
{{- $dashboards := tools "list_dashboards" "list_dashboard_snapshots" "grafana_search_dashboards" "grafana_list_folders" "grafana_list_folder_dashboards" -}}
{{- $dashboardReads := tools "get_dashboard" "get_dashboard_snapshot" "grafana_get_dashboard" -}}
{{- $serviceProfile := tools "get_service_profile" -}}
{{- $health := tools "get_service_summary" "get_service_performance_details" "get_service_operations_summary" "get_service_dependency_graph" -}}
{{- $alerts := tools "get_alerts" "get_alert_rule_state" -}}
{{- $changes := tools "get_change_events" -}}
{{- $deviations := tools "get_apm_service_deviations" "get_trace_attribute_deviations" -}}
{{- $scoped := tools "get_service_logs" "get_service_traces" "get_exceptions" "get_trace_waterfall" -}}
{{- $patterns := tools "get_log_patterns" "get_emerging_log_patterns" -}}
{{- $databases := tools "get_databases" "get_database_slow_queries" "get_database_queries" "get_database_server_metrics" -}}
{{- $profiles := tools "get_profile_summary" "get_top_functions" "get_flamegraph" -}}
{{- $raw := tools "get_logs" "get_traces" -}}
{{- $logInventory := "" -}}
{{- with $metricNames}}{{$raw = tools "prometheus_range_query" "prometheus_instant_query" "get_logs" "get_traces"}}{{else}}{{$logInventory = tools "prometheus_instant_query"}}{{end -}}
{{- $create := tools "create_dashboard" "create_alert" "create_notification_channel" "create_alert_from_chart" -}}
{{- $validate := tools "validate_dashboard" -}}
{{- $alertValidate := tools "validate_alert_expression" -}}
{{- $alertDesign := tools "recommend_alert_config" "describe_alert_chart" -}}
{{- $dropRules := tools "get_drop_rules" -}}
{{- $refine := tools "update_dashboard" "update_alert" "patch_alert" "attach_notification_channel" -}}
{{- $claim := tools "acknowledge_alert" "reassign_alert" -}}
{{- $addDropRule := tools "add_drop_rule" -}}
{{- $destructive := tools "update_dashboard" "delete_dashboard" "delete_dashboard_snapshot" "update_alert" "patch_alert" "delete_alert" "mute_alert_group" "detach_notification_channel" "delete_notification_channel" -}}
Last9 observability for the caller's organization. Discover names first, answer with the most specific tool, and treat writes with care.
{{- if or $services $attributes $metricNames $logInventory $alertRules $dashboards}}

## Discover before you query
Never guess service, environment, attribute, metric or label names, or dashboard ids; look them up and reuse the exact values.
{{- with $services}}
- Services, environments and datasources: {{.}}.
{{- end}}
{{- with $attributes}}
- Log and trace attributes: {{.}}.
{{- end}}
{{- with $metricNames}}
- Metric and label names: {{.}}.
{{- end}}
{{- with $logInventory}}
- Log-sending services and their indexes: {{.}} over `physical_index_service_count`; no other metric names can be discovered here.
{{- end}}
{{- with $alertRules}}
- Alert groups, rules, their KPIs and notification channels: {{.}}.
{{- end}}
{{- with $dashboards}}
- Dashboards, snapshots and Grafana folders: {{.}}{{with $dashboardReads}}; open one by id with {{.}}{{end}}.
{{- end}}
{{- end}}
{{- if or $serviceProfile $health $alerts $changes $deviations $scoped $patterns $databases $profiles}}

## Prefer dedicated tools
They return computed answers with the right scoping; reach for them before writing a query.
{{- with $serviceProfile}}
- Start a service investigation with {{.}}.
{{- end}}
{{- with $health}}
- Service health and performance: {{.}}.
{{- end}}
{{- with $alerts}}
- Firing alerts and rule state: {{.}}.
{{- end}}
{{- with $changes}}
- Deploys and other changes: {{.}}.
{{- end}}
{{- with $deviations}}
- What deviates from baseline: {{.}}.
{{- end}}
{{- with $scoped}}
- One service's logs, traces and errors: {{.}}.
{{- end}}
{{- with $patterns}}
- Log templates and what is new in them, before reading raw lines: {{.}}.
{{- end}}
{{- with $databases}}
- Databases: {{.}}.
{{- end}}
{{- with $profiles}}
- Profiles: {{.}}.
{{- end}}
{{- end}}
{{- with $raw}}

## Raw queries are the last resort
Use {{.}} only when no dedicated tool answers the question, and build each query from discovered names.
{{- end}}
{{- if or (has "get_logs") (has "get_traces") (has "get_service_logs") (has "prometheus_range_query") (has "get_service_profile")}}

## Reference manuals
Read these resources before writing a non-trivial query:
{{- if has "get_logs"}}
- `get_logs` pipelines: `last9://reference/logjson`
{{- end}}
{{- if has "get_traces"}}
- `get_traces` pipelines: `last9://reference/tracejson`
{{- end}}
{{- if has "get_service_logs"}}
- `get_service_logs` filters: `last9://reference/service_logs`
{{- end}}
{{- if has "prometheus_range_query"}}
- `prometheus_range_query` PromQL: `last9://reference/metrics`
{{- end}}
{{- if has "get_service_profile"}}
- Service investigations: `last9://reference/investigation`
{{- end}}
{{- end}}
{{- if or $validate $alertValidate $alertDesign $dropRules}}

## Check before you write
{{- with $validate}}
- Dry-run a dashboard with {{.}} before saving it.
{{- end}}
{{- with $alertDesign}}
- Design an alert rule with {{.}} before creating it.
{{- end}}
{{- with $alertValidate}}
- Dry-run an alert expression with {{.}} before creating or updating a rule.
{{- end}}
{{- with $dropRules}}
- List existing drop rules with {{.}} before adding one.
{{- end}}
{{- end}}
{{- if or $create $refine $claim $destructive $addDropRule}}

## Write tools
{{- with $create}}
- Create once with {{.}} and keep the returned id; never create a second copy to change the first.
{{- end}}
{{- with tools "create_alert"}}
- Alert rules from {{.}} are created disabled{{with tools "patch_alert"}}; enable them with {{.}} after validating{{end}}.
{{- end}}
{{- with $refine}}
- Refine an existing object with {{.}}.
{{- end}}
{{- with $claim}}
- Claim, release or hand off an alert with {{.}}; these change ownership, not data.
{{- end}}
{{- with $destructive}}
- Confirm with the user before overwriting or removing data with {{.}}.
{{- end}}
{{- with $addDropRule}}
- {{.}} drops matching future logs: confirm with the user first.
{{- end}}
{{- end}}
