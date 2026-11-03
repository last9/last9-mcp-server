# Worked dashboard examples

This prompt takes no arguments. Use these patterns after discovery, not as evidence that a tenant has these metrics or labels. The payload below is one complete example containing five sections. Its http_* metrics illustrate ordinary counters and classic histogram buckets, and node_* metrics illustrate a host exporter. Verify their existence, types, units and labels; adapt to the actual instrumentation before any write. Discovering, checking and saving it needs the dashboards and investigate toolsets (`--toolsets=dashboards,investigate`); if a tool is not available in this session, report what stays unverified or unsaved. Do not substitute derived trace_* interval counts into counter rate/increase expressions; see `query_guide`.

## Variables and dependent matches

The environment variable is discovered first. The service variable's matches array filters by $env; endpoint depends on both $env and $service; instance depends on $env. This is a dependency order, not four independent global label lists. Resolve each matches selector using the selected parent values, then discover source label values with `prometheus_label_values` (its argument is match_query, not matches). The dashboard variable itself uses matches, an array of selectors. Keep target names unique and dependencies acyclic.

Choose real current_values after discovery; none are fabricated below. These examples deliberately use single selection (multiple=false) so the JSON pipelines' equality predicates retain their meaning. For multiple selections, use regex matchers with escaped alternatives for PromQL and equivalent OR/regex filters for pipelines; test each selection and All. A failed lookup or empty current_values is not a valid default. Confirm that metric service/env labels identify the same scope as log and trace fields before sharing a variable across signals. The example uses the default datasource; if another is selected, bind panels' datasource_id to its discovered id and test using its name.

## RED per service

Three stat panels show requests/second, server-error percentage and rolling five-minute p95 latency. Counters use rate before sum; histogram quantiles retain le. The error percentage uses identical request populations and excludes zero denominators. Missing error series remain missing until coverage establishes a true zero. These instant panels describe the last five minutes at the selected end time, not an overall percentile across the dashboard's entire window.

## Latency by endpoint

A full-width timeseries preserves endpoint and service in the histogram grouping. It shows rolling five-minute p95 in seconds, not an average of endpoint percentiles. Do not convert to milliseconds without multiplying by 1000 and changing the unit together.

## Errors by status class

Two instant bar panels compare 4xx and 5xx totals per service over $__range. These are counts, not percentages or per-second rates. Resolve $__range to the requested duration for manual queries. Verify that status is the discovered HTTP status label and that both classes use the same server-request population.

## Host/infra health

Two timeseries show CPU busy percentage from an idle CPU-time counter and current available memory in bytes from a gauge. Keep instance identity; do not rate the memory gauge. If your exporter reports utilization directly, plot that gauge instead of using the counter formula.

## Log-count and trace panels

A scoped logjson window_aggregate counts all log entries per five-minute bucket; it is not an error count. A tracejson table computes the selected window's p95 raw span Duration by SpanName in nanoseconds. Discover the actual environment field first. For inbound endpoint latency, add a discovered server-span filter rather than mixing client and server spans. No raw log fetch is needed for the count chart.

```json
{
  "dashboard": {
    "name": "Service and infrastructure example",
    "variables": [
      {"id": "env", "display_name": "Env", "target": "env", "type": "label", "source": "env", "multiple": false, "matches": ["http_requests_total"]},
      {"id": "service", "display_name": "Service", "target": "service", "type": "label", "source": "service", "multiple": false, "matches": ["http_requests_total{env=~\"$env\"}"]},
      {"id": "endpoint", "display_name": "Endpoint", "target": "endpoint", "type": "label", "source": "endpoint", "multiple": false, "matches": ["http_request_duration_seconds_bucket{env=~\"$env\",service=~\"$service\"}"]},
      {"id": "instance", "display_name": "Instance", "target": "instance", "type": "label", "source": "instance", "multiple": false, "matches": ["node_cpu_seconds_total{env=~\"$env\"}"]}
    ],
    "panels": [
      {"name": "RED per service", "layout": {"x": 0, "y": 0, "w": 12, "h": 1}, "visualization": {"type": "section"}},
      {"name": "Requests per second", "version": 1, "layout": {"x": 0, "y": 1, "w": 4, "h": 4}, "visualization": {"type": "stat"}, "unit": "reqps", "queries": [{"name": "A", "type": "instant", "query_type": "promql", "telemetry": "metrics", "expr": "sum by (service) (rate(http_requests_total{env=~\"$env\",service=~\"$service\"}[5m]))", "legend": {"type": "auto", "value": ""}}]},
      {"name": "Server errors (%)", "version": 1, "layout": {"x": 4, "y": 1, "w": 4, "h": 4}, "visualization": {"type": "stat"}, "unit": "percent", "queries": [{"name": "A", "type": "instant", "query_type": "promql", "telemetry": "metrics", "expr": "(100 * sum by (service) (rate(http_requests_total{env=~\"$env\",service=~\"$service\",status=~\"5..\"}[5m])) / sum by (service) (rate(http_requests_total{env=~\"$env\",service=~\"$service\"}[5m]))) and on (service) (sum by (service) (rate(http_requests_total{env=~\"$env\",service=~\"$service\"}[5m])) > 0)", "legend": {"type": "auto", "value": ""}}]},
      {"name": "p95 request latency (seconds, last 5m)", "version": 1, "layout": {"x": 8, "y": 1, "w": 4, "h": 4}, "visualization": {"type": "stat"}, "unit": "s", "queries": [{"name": "A", "type": "instant", "query_type": "promql", "telemetry": "metrics", "expr": "histogram_quantile(0.95, sum by (le, service) (rate(http_request_duration_seconds_bucket{env=~\"$env\",service=~\"$service\"}[5m])))", "legend": {"type": "auto", "value": ""}}]},
      {"name": "Latency by endpoint", "layout": {"x": 0, "y": 5, "w": 12, "h": 1}, "visualization": {"type": "section"}},
      {"name": "p95 by endpoint (seconds, rolling 5m)", "version": 1, "layout": {"x": 0, "y": 6, "w": 12, "h": 8}, "visualization": {"type": "timeseries"}, "unit": "s", "queries": [{"name": "A", "type": "range", "query_type": "promql", "telemetry": "metrics", "expr": "histogram_quantile(0.95, sum by (le, service, endpoint) (rate(http_request_duration_seconds_bucket{env=~\"$env\",service=~\"$service\",endpoint=~\"$endpoint\"}[5m])))", "legend": {"type": "auto", "value": ""}}]},
      {"name": "Errors by status class", "layout": {"x": 0, "y": 14, "w": 12, "h": 1}, "visualization": {"type": "section"}},
      {"name": "4xx responses in selected window", "version": 1, "layout": {"x": 0, "y": 15, "w": 6, "h": 8}, "visualization": {"type": "bar"}, "unit": "short", "queries": [{"name": "A", "type": "instant", "query_type": "promql", "telemetry": "metrics", "expr": "sum by (service) (increase(http_requests_total{env=~\"$env\",service=~\"$service\",status=~\"4..\"}[$__range]))", "legend": {"type": "auto", "value": ""}}]},
      {"name": "5xx responses in selected window", "version": 1, "layout": {"x": 6, "y": 15, "w": 6, "h": 8}, "visualization": {"type": "bar"}, "unit": "short", "queries": [{"name": "A", "type": "instant", "query_type": "promql", "telemetry": "metrics", "expr": "sum by (service) (increase(http_requests_total{env=~\"$env\",service=~\"$service\",status=~\"5..\"}[$__range]))", "legend": {"type": "auto", "value": ""}}]},
      {"name": "Host/infra health", "layout": {"x": 0, "y": 23, "w": 12, "h": 1}, "visualization": {"type": "section"}},
      {"name": "CPU busy (%)", "version": 1, "layout": {"x": 0, "y": 24, "w": 6, "h": 8}, "visualization": {"type": "timeseries"}, "unit": "percent", "queries": [{"name": "A", "type": "range", "query_type": "promql", "telemetry": "metrics", "expr": "100 * (1 - avg by (instance) (rate(node_cpu_seconds_total{env=~\"$env\",instance=~\"$instance\",mode=\"idle\"}[5m])))", "legend": {"type": "auto", "value": ""}}]},
      {"name": "Available memory (bytes)", "version": 1, "layout": {"x": 6, "y": 24, "w": 6, "h": 8}, "visualization": {"type": "timeseries"}, "unit": "bytes", "queries": [{"name": "A", "type": "range", "query_type": "promql", "telemetry": "metrics", "expr": "node_memory_MemAvailable_bytes{env=~\"$env\",instance=~\"$instance\"}", "legend": {"type": "auto", "value": ""}}]},
      {"name": "Log-count and trace panels", "layout": {"x": 0, "y": 32, "w": 12, "h": 1}, "visualization": {"type": "section"}},
      {"name": "Log entries per 5 minutes", "version": 1, "layout": {"x": 0, "y": 33, "w": 6, "h": 8}, "visualization": {"type": "timeseries"}, "unit": "short", "queries": [{"name": "A", "type": "range", "query_type": "log_json", "telemetry": "logs", "expr": "[{\"type\":\"filter\",\"query\":{\"$and\":[{\"$eq\":[\"ServiceName\",\"$service\"]},{\"$eq\":[\"resources['deployment.environment']\",\"$env\"]}]}},{\"type\":\"window_aggregate\",\"function\":{\"$count\":[]},\"as\":\"count\",\"window\":[\"5\",\"minutes\"],\"groupby\":{\"ServiceName\":\"service\"}}]", "legend": {"type": "auto", "value": ""}}]},
      {"name": "p95 span duration by endpoint (nanoseconds)", "version": 1, "layout": {"x": 6, "y": 33, "w": 6, "h": 8}, "visualization": {"type": "table"}, "unit": "ns", "queries": [{"name": "A", "type": "instant", "query_type": "trace_json", "telemetry": "traces", "expr": "[{\"type\":\"filter\",\"query\":{\"$and\":[{\"$eq\":[\"ServiceName\",\"$service\"]},{\"$eq\":[\"resources['deployment.environment']\",\"$env\"]}]}},{\"type\":\"aggregate\",\"aggregates\":[{\"function\":{\"$quantile\":[0.95,\"Duration\"]},\"as\":\"p95_duration_ns\"}],\"groupby\":{\"SpanName\":\"endpoint\"}}]", "legend": {"type": "auto", "value": ""}}]}
    ]
  },
  "metadata": {"_category": "custom", "_type": "metrics"}
}
```

Before approval, follow `create_dashboard_guide`: validate the full dashboard object and run every query with resolved variables over the requested window using `prometheus_range_query`, `prometheus_instant_query`, `get_logs` or `get_traces`. The trace check is required even if `validate_dashboard` reports unsupported_execution. Verify units, cardinality, nonempty defaults and percentage bounds. Then create once, retain the id and use `patch_dashboard` for small edits; read the saved result with `get_dashboard`.
