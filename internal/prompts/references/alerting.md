# Alert Authoring Reference

Use with `create_alert`, `update_alert`, `validate_alert_expression`, and `recommend_alert_config`.
Read this when writing or refining alert rules; keep tool descriptions for parameter shapes.

## KPI discovery

1. Find the alert group with `get_alert_groups` / `get_alert_config`.
2. List KPIs with `get_entity_kpis` (id, name, query, unit). Prefer this over inventing IDs.
3. If no suitable KPI exists, verify PromQL with `prometheus_range_query`, then `create_kpi`.
4. Bind KPI ids in `expression_args` / `indicators_used`. `get_entity_alert_rules` only shows KPIs already used by a rule.

Units matter: thresholds must use the same unit as the KPI (seconds vs milliseconds, ratio vs percent).

## Expression grammar

Expressions are arithmetic over **indicator names** (not raw PromQL). Indicator names map to KPI UUIDs via `expression_args` / `indicators_used`.

- Static: plain arithmetic over up to 3 indicators, e.g. `x`, `x / y`, `(errors / total) * 100`. Expression may be omitted when the primary indicator alone is the signal.
- Adaptive (only documented form): exactly
  `adaptive_std_cmp(<primary_indicator>, sensitivity, window)`
  e.g. `adaptive_std_cmp(latency, 3, 1h)`.
  - `sensitivity`: number (dashboard uses whole numbers 1–10, default 3)
  - `window`: baseline length (dashboard offers `1h`, `1h30m`, `2h`, `2h30m`, `3h`)
  - Do not document or invent other adaptive macros.

## Static vs adaptive fields

| Field | Static | Adaptive (`adaptive_std_cmp`) |
|---|---|---|
| `expression` | Arithmetic over indicators, or omit | Exactly `adaptive_std_cmp(...)` |
| `condition` | **Required** (e.g. `expr > 0.05`) | Optional; default `expr > 0` |
| `alert_condition` | **Required** (bad/total minutes model) | Optional; default `count_true(result) >= 1` |
| `eval_window` | **Required**, 0–60 minutes | Optional; default `1` |

There is **no Prometheus `for:` clause**. Firing uses the bad-minutes / total-minutes model via `alert_condition` over `eval_window`:

- Example: `count_true(result) >= 3` with `eval_window: 5` → fire when at least 3 of the last 5 one-minute evaluations are true.
- Clients cannot set `algorithm`; the backend derives it from `expression`.

## validate_alert_expression

- Dry-run only; does not save.
- Pass the **inner metric expression** (arithmetic over indicators). Do **not** wrap in `adaptive_std_cmp(...)` — wrappers fail to parse.
- `indicators_used` maps each indicator name in the expression → KPI UUID owned by `entity_id`.
- Checks: expression parseability and that each mapped KPI exists on the group.
- Does **not** check: thresholds are grounded, units match, PromQL returns data, or adaptive wrapper shape.
- Result: `{"valid":true}` or `{"valid":false,"reason":"..."}`. `"invalid indicator details for X"` means that UUID is not a KPI of this alert group. Invalid expressions are normal results, not tool errors.

## Lifecycle

1. `recommend_alert_config` → design
2. Discover/create KPIs → ground thresholds with `prometheus_range_query`
3. `validate_alert_expression` until `valid:true`
4. `create_alert` (always a disabled draft)
5. Read back with `get_alert_config` / `get_entity_alert_rules`
6. Promote with `patch_alert` `is_disabled=false` **only after explicit user confirmation**
7. Refine with `update_alert` (full replace) using the returned rule id — never create a duplicate

## Alert from a dashboard panel

`create_alert_from_chart` covers Discover service/exceptions charts only. For a custom dashboard panel (`create_dashboard` / `get_dashboard`):

1. `get_dashboard` → take the panel's PromQL
2. Choose an alert group; `create_kpi` with that query (after `prometheus_range_query` verifies data)
3. `create_alert` draft bound to the new KPI
4. Read back → promote with `patch_alert` only after confirmation

## Templates

### Latency (static)

```
primary_indicator: latency
expression: (omit or) latency
condition: expr > 0.5
alert_condition: count_true(result) >= 3
eval_window: 5
expression_args: { "latency": { "id": "<kpi-uuid>" } }
```
Unit must match the KPI (e.g. seconds). Threshold from observed p99 via `prometheus_range_query`, not invented.

### Latency (adaptive)

```
primary_indicator: latency
expression: adaptive_std_cmp(latency, 3, 1h)
# condition / alert_condition / eval_window optional
expression_args: { "latency": { "id": "<kpi-uuid>" } }
```
Validate the **inner** name only if you dry-run arithmetic; adaptive wrappers are not passed to `validate_alert_expression`.

### Error rate (static)

```
primary_indicator: errors
expression: errors / total
condition: expr > 0.01
alert_condition: count_true(result) >= 3
eval_window: 5
expression_args:
  errors: { id: "<errors-kpi>" }
  total:  { id: "<total-kpi>" }
indicators_used (validate): { "errors": "<errors-kpi>", "total": "<total-kpi>" }
```

### Error rate (adaptive)

```
expression: adaptive_std_cmp(error_rate, 3, 1h)
primary_indicator: error_rate
```

### Saturation (static)

```
primary_indicator: saturation
expression: saturation
condition: expr > 0.85
alert_condition: count_true(result) >= 3
eval_window: 5
```
Use a unit-consistent capacity ratio (0–1) or percent — match the KPI unit.
