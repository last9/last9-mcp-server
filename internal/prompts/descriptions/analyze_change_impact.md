Assess whether a deployment or change likely regressed one service by comparing evidence before and after its timestamp.

Pass exactly one identity form: either a `change_event` returned by `get_change_events`, or `service` plus `timestamp` (RFC3339 or Unix seconds). Do not combine forms. Optional `env` narrows evidence; optional window arguments use the units and bounds in the input schema.

Use the returned verdict and evidence; do not recompute thresholds. Missing or partial evidence is unknown, not healthy. Trace attribute cohorts are supporting context and do not establish cause or gate the verdict.

## When to use
Determine whether a known change coincides with a service regression.

## When not to use
- Discover changes first: `get_change_events`
- Compare arbitrary equal windows: `get_apm_service_deviations`
- Investigate without a known change time: `get_service_profile`

## Pairs well with
- `get_change_events`
- `get_service_performance_details`
- `get_service_dependency_graph`

## Output shape
Verdict, scoped before/after windows, regressions, signal evidence, limitations and drill-downs.

## Known traps
Read `last9://reference/tool_recipes` before interpreting mixed or partial evidence.
