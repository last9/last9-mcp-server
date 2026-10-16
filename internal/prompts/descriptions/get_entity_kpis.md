List every KPI on an alert group (entity): id, name, PromQL/query, and unit.

Use this before create_alert / validate_alert_expression when you need KPI UUIDs.
get_entity_alert_rules only shows KPIs already bound to a rule; this tool lists all
KPIs on the group, including unused ones. Prefer get_entity_kpis over inventing IDs.

Input:
- entity_id (required): alert group/entity UUID from get_alert_groups or get_alert_config

Output per KPI: id, name, query (PromQL), unit.

To add a KPI, use create_kpi (alerts toolset). Verify the query returns data with
prometheus_range_query first. Units must match the threshold you plan to set.
