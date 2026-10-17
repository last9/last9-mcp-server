Workflow: author a Last9 alert rule for: {{.goal}}{{if .alert_group}} (alert group "{{.alert_group}}"){{end}}.

Read last9://reference/alerting first for the expression grammar, the static vs adaptive fields, units and rule templates. Each gate below is mandatory; do not skip one to save a call.

Steps:
1. Find the alert group: {{if .alert_group}}call get_alert_groups(alert_group_name="{{.alert_group}}") and take the matching group's id as entity_id{{else}}call get_alert_groups and pick the group that owns this signal as entity_id; if more than one fits, ask the user{{end}}.
2. {{if .dashboard_id}}Alert from a dashboard panel: call get_dashboard(id="{{.dashboard_id}}"), pick the panel{{if .panel}} "{{.panel}}"{{end}}, take its PromQL expr and resolve any dashboard variables ($service, $env, ...) to concrete values. Then call get_entity_kpis(entity_id) and reuse a KPI with the same query; only if none exists, call create_kpi once with that PromQL and the panel's unit, and keep the returned id. Never invent a KPI id.{{else}}Call get_entity_kpis(entity_id) and pick the KPI(s) whose query measures the goal. Only if none fits, check the query you need returns data with prometheus_range_query, then call create_kpi once and keep the returned id. Never invent a KPI id.{{end}}
3. Ground the threshold in real data: call prometheus_range_query on each KPI's query over a representative window (at least the last 24h) and note typical and peak values in the KPI's unit. The threshold compares against the raw query value; the unit converts nothing.
4. Call recommend_alert_config(signal, objective) for the design checklist, and choose a static or adaptive rule.
5. Call validate_alert_expression(entity_id, expression=<inner metric expression, never the adaptive_std_cmp wrapper>, indicators_used={KPI name: KPI id}); key each indicator by its KPI's exact name, as create_alert requires the same names in expression_args. If it returns valid:false, fix the cause and call it again. Do not continue until it returns {"valid":true}.
6. Call create_alert once with entity_id and the full alert_rule. It saves a disabled draft; keep the returned rule id. To change the rule afterwards use update_alert (keep the replacement id it returns) or patch_alert on that id; never call create_alert again for this rule.
7. Read it back: call get_entity_alert_rules(entity_id) and confirm the saved expression, condition, alert_condition, eval_window and indicators match what you intended. Fix any difference with update_alert.
8. Show the user the draft: rule id, expression, threshold with its unit, and evaluation window. Call patch_alert(entity_id, id, is_disabled=false) only after the user explicitly confirms; otherwise stop and report the draft id.

Do not call mute_alert_group, and do not change notification channels, in this workflow.
