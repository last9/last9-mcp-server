List every KPI (indicator) defined on one alert group, including KPIs no alert rule uses yet. Use it to get the KPI UUIDs that alert rules and `validate_alert_expression` need in `expression_args` / `indicators_used`; never invent a KPI id or reuse one from another alert group.

Parameters:
- `entity_id` (required): alert group/entity UUID, from `get_alert_groups` or `get_alert_config`.

Returns JSON `{"entity_id", "count", "kpis": [{"id", "name", "description", "query", "unit"}]}`, sorted by name. `query` is the KPI's PromQL; check it returns data with `prometheus_range_query` before alerting on it. `unit` is a display label only: thresholds are compared against the raw query value, so a query in seconds needs a threshold in seconds even if the unit says ms. Alert authoring guide: `last9://reference/alerting`.

## Pairs well with
- `create_kpi` (when no existing KPI fits)
- `validate_alert_expression` (dry-run an expression over these KPIs)
