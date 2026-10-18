Create a KPI (indicator) on an existing alert group so alert rules can bind to it. Use it only when `get_entity_kpis` shows no KPI with the query you need.

On a timeout or connection error the create may or may not have been applied; check by name with get_entity_kpis before retrying.

Before creating: run the query with `prometheus_range_query` over a representative window and confirm it returns data with the series and labels you expect. The API rejects only a query that does not parse; it saves a valid query for a metric that does not exist.

Parameters:
- `entity_id` (required): alert group/entity UUID.
- `name` (required): KPI name, unique within the alert group; a second create with the same name is rejected as a conflict.
- `query` (required): PromQL. To alert on a dashboard panel, take the panel's PromQL from `get_dashboard`.
- `unit` (required): unit of the query's raw value, for example `seconds`, `ms`, `percent`, `ratio`, `count`, `req/s`. Units matter: the unit is a label only and converts nothing, so alert thresholds are compared against the raw query value. A latency query in seconds needs `expr > 0.5`, not `expr > 500`.
- `description` (optional): what the KPI measures.
- `data_source_id` (optional): data source UUID; defaults to the alert group's data source.

A created KPI is permanent here: no MCP tool deletes or edits a KPI (that needs the Last9 UI), so confirm name, query and unit before calling. Create once and keep the returned id: bind it in `expression_args` (`create_alert`, `update_alert`) and `indicators_used` (`validate_alert_expression`). Never call create_kpi again for the same query; on a name conflict, call `get_entity_kpis` and reuse the existing id. Returns `{"id", "name", "query", "unit"}` and a link to Alerting in the result's _meta.reference_url. Alert authoring guide: `last9://reference/alerting`.