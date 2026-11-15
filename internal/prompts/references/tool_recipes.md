# Tool recipes and interpretation

Read this reference only when a result needs deeper interpretation or you need a worked investigation or authoring recipe. Tool descriptions retain required arguments, formats, defaults, and safety constraints.

## APM service deviations

Use returned percentages and deltas; never calculate percentage changes from raw counts, latency, Apdex, or samples. `regressions` and `improvements` are separate, while throughput is contextual. State evidence quality and any `partial_errors`. If every metric query fails, the tool errors.

`operation_apdex_reconciliations` covers only returned operations. Report both request-coverage values and `unexplained_delta`; the residual can include unreported operations and population differences. Correlation does not establish contribution, attribution, cause, or root cause. Stable, `no_data`, and `unsupported_workload_shape` are terminal unless the user requested deeper investigation.

## Alert inventory

`get_alert_config` channel filters inspect per-entity bindings, not global channels. Types and missing-channel filters are OR-combined; names and severities constrain the same binding row. Recent evaluations contain five newest-first one-minute samples and may be omitted for more than 25 enabled matches. Use `get_entity_alert_rules` for PromQL and full properties, `get_alert_rule_state` for longer history, and `get_alert_groups` for zero-rule groups.

## Exception investigation

Exception rows are metric aggregates, never exact trace samples. For a service, consult `get_service_profile` to decide whether span exceptions are likely the answer or a symptom. Use `get_service_traces` with the same window, service, and environment for contextual traces. For log-heavy services, aggregate errors with `get_logs` first, narrow to a logger, then fetch a small raw sample with an explicit limit. Use `get_service_logs` only for HTTP status-class searches.

## Service summary

Language routing: HTTP 5xx → `http_5xx_count`; HTTP 4xx → `http_4xx_count`; gRPC errors → `grpc_error_count`; rpm → `throughput_rpm`; busiest or omitted → `request_count`. Counts are interval totals. Do not add error classes. Missing classes are zero; absent log-only services are not zero. If truncated, raise the limit toward `min(matched_count, 100)`.

## Trace attribute deviations

Compare returned current and baseline cohorts without deriving causal claims. Rank by the returned score and preserve evidence limitations. Attribute values can reveal correlated cohorts, not root cause. Follow promising cohorts with scoped trace or log queries only after inspecting the result.

## Log query recipes

For counts, use `aggregate` with `$count`; for buckets, use `window_aggregate`. A five-minute count stage is `{"type":"window_aggregate","function":{"$count":[]},"as":"count","window":["5","minutes"]}`. For percentile buckets, filter numeric strings with the canonical anchored regex, then use `$quantile` over raw values. Run one call over the full half-open range; never merge percentile rows. Read `last9://reference/logjson` for the full grammar and examples.

## Change impact

Use the report's verdict and evidence rather than recomputing thresholds. A likely regression can include traffic, error-rate, latency, alert, APM, and dependency evidence. Partial sections remain usable when their errors are disclosed. Treat trace cohorts as supporting context, not verdict gates or proof of cause.

## Dashboard authoring

Read `last9://reference/create_dashboard_guide` for the end-to-end workflow, `last9://reference/dashboard_examples` for payloads, and `last9://reference/query_guide` for query semantics. Create once, retain the id, preview or patch targeted changes, and use full update only to replace most of a dashboard.

## Alert authoring

Read `last9://reference/alerting` for expression grammar, units, field matrices, and templates. Create rules once as disabled drafts, retain the id, validate and read back, then refine with `patch_alert` or `update_alert`. Discover-chart alerts use only the catalog path through `create_alert_from_chart`; pasted PromQL does not change that route.
