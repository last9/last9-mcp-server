Assess whether a deployment or change likely regressed one service by comparing evidence before and after its timestamp.

Pass exactly one identity form: either a `change_event` returned by `get_change_events`, or `service` plus `timestamp` (RFC3339 or Unix seconds). Do not combine forms. The timestamp is rounded down to the minute. Baseline minutes default 60; baseline and after windows allow 30..120, with at least 5 minutes after the change.

The verdict is one of `regression likely`, `no significant change`, or `insufficient data`; each is a valid answer. Use it without recomputing thresholds. Fallback regression thresholds require 100 requests per window and include error rate up 1 percentage point, p95 up at least 40%, or throughput (rpm) down at least 50%. Evidence is insufficient if a required window is missing wholly or in any query listed in `deviations.apm.unavailable`.

Scope: with no `env` arg, error rate and throughput sum every env, `""`, while p95 uses `latency_env`. The server-error signal also counts error-status spans and 429s; client-only ones show 0 rpm. Alerts match service by label, else annotation; a new alert still firing at the after window's end (`reason` counts resolved ones) can set the verdict. Trace-attribute deviations explain a verdict but never set one.

`regressions` contains the top 10 items, while evidence sections keep 10 per list. Each regression has exactly one of `error_rate_pct`, `latency_p95_ms`, `throughput_rpm`, `alert`, `apm_deviation`, a role (`changed` or `dependent`), and an environment; alert evidence adds `rule_id` and `state`. Missing or partial evidence is unknown, not healthy.

## When to use
Determine whether a known change coincides with a service regression.

## When not to use
- Discover changes first: `get_change_events`

## Pairs well with
- `get_change_events`

## Output shape
Verdict, scoped before/after windows, regressions, signal evidence, limitations and drill-downs.

## Known traps
Read `last9://reference/tool_recipes` before interpreting mixed or partial evidence.
