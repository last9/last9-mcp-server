Author an alert rule as a disabled draft, then promote only after the user confirms.

Goal: {{.goal}}
{{if .entity_id}}Alert group entity_id: {{.entity_id}}{{else}}Discover the alert group with get_alert_groups / get_alert_config first; do not invent entity_id.{{end}}
{{if .signal}}Signal hint: {{.signal}}{{end}}

Steps (stop and ask before promote):

1. Resolve the alert group (entity_id). Prefer an existing sandbox/group named for this work when available.
2. Discover KPIs with get_entity_kpis. If none fit, verify PromQL via prometheus_range_query, then create_kpi once and keep the returned id. Units must match the threshold you will set.
3. Ground the threshold (or adaptive sensitivity) with real data from prometheus_range_query — never invent numbers.
4. Call recommend_alert_config for static vs adaptive shape and responder context.
5. Call validate_alert_expression with the inner metric expression and indicators_used (indicator name → KPI id) until valid:true. Do not wrap adaptive_std_cmp in the validator.
6. create_alert as a draft (always disabled). Keep the returned rule id; never create a duplicate for updates — use update_alert.
7. Read back with get_alert_config (check recent evaluation history and classification) and/or get_entity_alert_rules.
8. Promote with patch_alert is_disabled=false **only after explicit user confirmation**. If the user has not confirmed, stop.

Dashboard panel path: if the signal comes from a custom dashboard (get_dashboard / create_dashboard), take the panel PromQL → create_kpi on the chosen group → create_alert. create_alert_from_chart is Discover-only.

Must-fail behaviors (never do these): invent KPI ids; create_alert after valid:false; promote without confirmation; create a second rule instead of update_alert; skip read-back.

Full grammar and templates: read last9://reference/alerting.
