# Alert authoring reference

How to write a Last9 alert rule that validates, saves, and fires on the right thing. Tools: `get_alert_groups`, `get_entity_kpis`, `create_kpi`, `prometheus_range_query`, `recommend_alert_config`, `validate_alert_expression`, `create_alert`, `update_alert`, `patch_alert`, `get_entity_alert_rules`, `get_alert_config`.

## Model

An **alert group** (entity, `entity_id`) owns **KPIs** (indicators). A KPI is a PromQL query plus a unit. An **alert rule** belongs to one alert group and binds one to three of that group's KPIs:

- `expression_args` maps an **indicator name** to `{"id": "<kpi-uuid>"}`. Each key must be the exact name of the KPI it binds (as `get_entity_kpis` lists it), not a placeholder such as `x`: `create_alert` and `update_alert` reject any other key. `expression` and `primary_indicator` use the same names; write names with spaces, dots or dashes as-is (`Test Alert / errors`).
- `primary_indicator` must be one of the `expression_args` keys.
- Every KPI id must belong to the same `entity_id`. Get ids from `get_entity_kpis`; never invent one, and never reuse an id from another alert group. If no KPI fits, create one with `create_kpi` and keep its id.

Notification routing is configured on the alert group, not the rule.

## Expression grammar

`expression` is arithmetic over indicator names, evaluated per series:

- Operators: `+ - * / %`, parentheses, and numeric literals. Example: `errors / total`, `(used / capacity) * 100`.
- One to three distinct indicator names. More than three is rejected.
- Every name in the expression needs an entry in `expression_args` (and in `indicators_used` when validating).
- Omit `expression` for a single-indicator static rule; the primary indicator's value is used.

The only adaptive form is:

```
adaptive_std_cmp(<primary_indicator>, <sensitivity>, <window>)
```

- `<primary_indicator>` is the indicator name, not an arithmetic expression. To alert adaptively on a ratio, build the ratio into one KPI's query.
- `<sensitivity>` is a number; the dashboard uses whole numbers 1-10, default 3.
- `<window>` is the baseline length: `1h`, `1h30m`, `2h`, `2h30m`, or `3h`.

Clients cannot set `algorithm`; the backend derives it from `expression`.

## Condition fields

A rule evaluates once a minute. `condition` turns the expression value (`expr`) into true/false for that minute; `alert_condition` decides whether enough recent minutes were bad; `eval_window` is how many recent minutes count.

This is a bad-minutes / total-minutes model: `alert_condition: "count_true(result) >= <bad_minutes>"` with `eval_window: <total_minutes>` fires when the condition held in at least `bad_minutes` of the last `total_minutes` minutes. There is **no `for:` clause**; "for 5 minutes" is `count_true(result) >= 5` with `eval_window: 5`.

| field | static rule | adaptive rule |
|---|---|---|
| `expression` | optional; arithmetic over indicators | required; exactly `adaptive_std_cmp(...)` |
| `condition` | **required**, e.g. `expr > 0.5` | optional; default `expr > 0` |
| `alert_condition` | **required**, e.g. `count_true(result) >= 3` | optional; default `count_true(result) >= 1` |
| `eval_window` | **required**, minutes, 0-60 | optional; default `1` |
| `algorithm` | ignored; never send it | ignored; never send it |
| `is_disabled` | ignored by `create_alert` (always a draft) | same |

Adaptive rules are evaluated as a threshold on the adaptive bounds; supplied `condition`, `alert_condition` and `eval_window` are kept, omitted ones take the defaults above.

Start static rules at `count_true(result) >= 3` over `eval_window: 5` and tune from observed incidents.

## Units

The KPI `unit` is a label. Nothing converts values: `condition` compares against the raw value the KPI query returns.

- A latency query in seconds needs `expr > 0.5` for 500 ms. Writing `expr > 500` never fires.
- A ratio query (0-1) needs `expr > 0.05` for 5%; a percent query (0-100) needs `expr > 5`.
- Read the unit and a sample of real values with `prometheus_range_query` before choosing a threshold, and say the unit next to every threshold you report.

## validate_alert_expression

A dry run that saves nothing. Use it before every `create_alert` and `update_alert`, and repeat it until it returns `{"valid":true}`.

- Pass the **inner metric expression** (`errors / total`, or just `latency`), never the `adaptive_std_cmp(...)` wrapper; wrappers fail to parse.
- `indicators_used` maps each indicator name to its KPI id: `{"errors": "<kpi-uuid>", "total": "<kpi-uuid>"}`. It is the same mapping as `expression_args`, flattened to ids. The dry run accepts any name, so key it by the KPI names `create_alert` will require.

It checks, in order:
1. every id in `indicators_used` is a KPI of `entity_id` — otherwise `invalid indicator details for <name>` (the id is invented, mistyped, or from another group: re-read `get_entity_kpis`);
2. the expression parses and uses one to three indicators;
3. every name in the expression is in `indicators_used` — otherwise `missing indicator details for <name>`;
4. only when more than one indicator is used: the KPI queries return at least one series together over the last hour — otherwise `expression yields 0 timeseries` (their label sets do not match; align the `by (...)` clauses).

It does **not** check `condition`, `alert_condition`, `eval_window`, the adaptive wrapper, units, whether a threshold is sensible, or (for a single indicator) whether the query returns data. Check those yourself with `prometheus_range_query`.

`{"valid":false,"reason":...}` is a normal result, not a tool error. Never call `create_alert` or `update_alert` after a `valid:false` without fixing the cause and validating again.

## Templates

Replace `<...>` with values discovered for this alert group. Find metric names with `prometheus_labels` / `prometheus_label_values` or an existing dashboard panel; do not guess them.

### Latency

KPI `latency`, unit `seconds`: `histogram_quantile(0.99, sum by (le) (rate(<duration_histogram>_bucket{<selector>}[5m])))`.

Static: fire when p99 is above 500 ms for 3 of 5 minutes.

```json
{"primary_indicator": "latency", "expression_args": {"latency": {"id": "<kpi-uuid>"}},
 "condition": "expr > 0.5", "alert_condition": "count_true(result) >= 3", "eval_window": 5,
 "severity": "breach", "rule_name": "<service> p99 latency > 500ms"}
```

Adaptive: fire when p99 leaves its learned 2-hour band.

```json
{"primary_indicator": "latency", "expression_args": {"latency": {"id": "<kpi-uuid>"}},
 "expression": "adaptive_std_cmp(latency, 3, 2h)", "severity": "threat",
 "rule_name": "<service> p99 latency anomaly"}
```

### Error rate

Two KPIs with the same `by (...)` labels, unit `req/s`: KPI `errors` `sum by (<labels>) (rate(<requests_total>{<selector>, <error_matcher>}[5m]))` and KPI `total` `sum by (<labels>) (rate(<requests_total>{<selector>}[5m]))`.

Static: fire when more than 5% of requests fail for 3 of 5 minutes. Validate `errors / total` with both ids.

```json
{"primary_indicator": "errors",
 "expression_args": {"errors": {"id": "<errors-kpi-uuid>"}, "total": {"id": "<total-kpi-uuid>"}},
 "expression": "errors / total", "condition": "expr > 0.05",
 "alert_condition": "count_true(result) >= 3", "eval_window": 5, "severity": "breach",
 "rule_name": "<service> error rate > 5%"}
```

Adaptive: `adaptive_std_cmp` takes one indicator, so use a single KPI `error_ratio` whose query is the ratio (unit `ratio`), for example `sum(rate(<requests_total>{<selector>, <error_matcher>}[5m])) / sum(rate(<requests_total>{<selector>}[5m]))`.

```json
{"primary_indicator": "error_ratio", "expression_args": {"error_ratio": {"id": "<ratio-kpi-uuid>"}},
 "expression": "adaptive_std_cmp(error_ratio, 3, 1h)", "severity": "threat",
 "rule_name": "<service> error rate anomaly"}
```

### Saturation

KPI `cpu`, unit `ratio` (0-1): for example `sum by (<labels>) (rate(<cpu_seconds_total>{<selector>}[5m])) / sum by (<labels>) (<cpu_limit>{<selector>})`.

Static: fire when above 85% for 8 of 10 minutes.

```json
{"primary_indicator": "cpu", "expression_args": {"cpu": {"id": "<kpi-uuid>"}},
 "condition": "expr > 0.85", "alert_condition": "count_true(result) >= 8", "eval_window": 10,
 "severity": "threat", "rule_name": "<workload> CPU > 85%"}
```

Adaptive: fire when utilization leaves its learned 3-hour band.

```json
{"primary_indicator": "cpu", "expression_args": {"cpu": {"id": "<kpi-uuid>"}},
 "expression": "adaptive_std_cmp(cpu, 4, 3h)", "severity": "threat",
 "rule_name": "<workload> CPU anomaly"}
```

Add responder context to any template under `properties`: `description`, `runbook: {"link": "https://..."}`, and `annotations` (owner, service, environment, summary, dashboard).

## Lifecycle

1. Find the alert group: `get_alert_groups` (or `get_alert_config` for an existing rule's `entity_id`).
2. Get its KPIs with `get_entity_kpis`; create a missing one with `create_kpi`, once.
3. Ground the threshold in real data: run each KPI query with `prometheus_range_query` over a representative window and note typical and peak values in the KPI's unit.
4. `recommend_alert_config` for the design checklist.
5. `validate_alert_expression` until `{"valid":true}`.
6. `create_alert` once. It always saves a disabled draft; keep the returned rule id. To change it, use `update_alert` (full replace; use the replacement id it returns) or `patch_alert`; never call `create_alert` again for the same rule.
7. Read it back with `get_entity_alert_rules` (or `get_alert_config` with `rule_id`) and confirm the expression, condition, indicators and unit match what you intended.
8. Promote with `patch_alert is_disabled=false` only after the user explicitly confirms. Without confirmation, report the draft id and stop.

## Alert from a dashboard panel

1. `get_dashboard` and pick the panel; take its PromQL `expr`. Panels using log or trace queries cannot back a KPI.
2. Resolve dashboard variables (`$service`, `$env`, ...) to concrete values; a KPI query cannot contain unresolved variables.
3. Choose the alert group the alert belongs to (`get_alert_groups`), then check `get_entity_kpis` for a KPI with the same query.
4. If none exists, `create_kpi` on that group with the panel's PromQL and the panel's unit.
5. Continue from step 3 of the lifecycle: ground the threshold, validate, `create_alert` as a draft, read it back, promote only on confirmation.

Dashboard tools do not create alerts themselves.
