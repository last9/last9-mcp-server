Create a net-new dashboard with `{dashboard, metadata}`. `dashboard` needs `name` and `panels`. When the user provides only a name, use `panels: []` and never invent queries.

Panels need `name` and `visualization: {type}`. Query and CSV panels need `layout: {x, y, w, h}`; omit layout from section/markdown panels. Queries belong only on query panels. CSV panels use `csv` plus dashboard-level `csv_datasource`. Non-section panels default to `version: 1`; logs panels must set it, sections stay versionless. Logs need `logs_config.columns` and exactly one `log_ql` or `log_raw` query. Do not use `gridPos` or a panel-level `type`.

Queries need `name`, `type`, `query_type`, `telemetry`, `expr`, and `legend: {type, value}`. Pair `promql`/metrics, `log_ql` or `log_json`/logs, and `trace_ql` or `trace_json`/traces. For `log_json` line, timeseries, or bar panels, use one `window_aggregate` with `window`; a plain `aggregate` stage does not satisfy the API's timeslice requirement, and grouping by service or environment does not replace the window. Example: `[{"type":"filter","query":{"$and":[{"$eq":["ServiceName","svc"]},{"$eq":["resources['deployment.environment']","prod"]}]}},{"type":"window_aggregate","function":{"$count":[]},"as":"count","window":["1","minutes"]}]`. For log panels: aggregate expr only, short lookback.

If the user supplies a service, use it without calling `get_service_profile`, `get_service_environments`, or `did_you_mean`. Validate with `validate_dashboard` and run each panel's query before approval. Create once, keep `dashboard.id`, and do not call create_dashboard again. Refine targeted panels through the route below; use `update_dashboard` only for full replacement.

Read prompts `create_dashboard_guide`, `dashboard_examples`, and `query_guide`; the same guides are registered as `last9://reference/create_dashboard_guide`, `last9://reference/dashboard_examples`, and `last9://reference/query_guide`. Read `last9://reference/tool_recipes` for lifecycle routing.

## When to use
Build a net-new dashboard with no id.

## When not to use
- Edit panels of an existing dashboard: `patch_dashboard`
- Restructure an existing dashboard: `update_dashboard`

## Pairs well with
- `validate_dashboard`
- `patch_dashboard` (edit panels by the returned id)

## Output shape
The created dashboard including its `id`.

## Known traps
On a timeout or connection error the create may or may not have been applied; check by name with list_dashboards before retrying.
