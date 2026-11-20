Return the Discover Services query recipe for a trace-derived APM metric: the query the Last9 dashboard runs, its unit, how it rolls up, and the functions that must not wrap it.

Call it before writing PromQL over a `trace_*` metric (throughput, latency, apdex). These series are per-minute gauges, not counters or classic histograms, so `rate`, `sum_over_time(...)/300` and `histogram_quantile` give wrong numbers.

Parameters:
- metric: (Required) Bare metric name, e.g. `trace_endpoint_count`. A PromQL selector or expression is rejected. Only `trace_*` names are looked up; any other name returns `uncovered` without calling the API.

Returns JSON with `metric` and `source`:
- `service_recipe`: the catalog has this metric. Fields: `indicator`, `unit` (e.g. `rpm`, `ms`), `recommended_query` (run this), `filters` (matchers the recipe always carries), `quantile_label` (latency only), `queries.chart` / `queries.list` (each `{rollup, promql}`; rollup `none` means use the series as-is; list `promql` contains `{duration}`, in minutes; substitute it before running), `forbidden_functions`.
- `legacy_heuristic`: the catalog could not be read; only for `trace_endpoint_count` and `trace_service_response_time`. Fields: `unit`, `example_query`, optional `counter_rule_query`, `basis`, `note`. This is the old manual example, not the dashboard query; say so if you use it.
- `uncovered`: no recipe for this metric (non-`trace_*`, not in the catalog, or catalog unreadable). No query; `note` says why.

Add environment, service or other label filters to the recipe's selectors yourself; the recipe is service-wide.
