Answers "did the change at T break anything?" for one service by comparing the window before a change with the window after it. Use it once `get_change_events` (or the user) names a deploy or config change near a symptom, or to confirm a change was safe.

## Parameters

- `change_event`: one element of `get_change_events` `change_events[]`, passed unchanged. Uses `metric.service`, `metric.deployment_environment` and the earliest `values[].timestamp`.
- `service` + `timestamp`: the alternative to `change_event`; `timestamp` is RFC3339 or unix seconds. Pass exactly one form.
- `env`: exact environment (default: the event's, else all).
- `baseline_minutes`: minutes before T, default 60, clamped to 30..120.
- `after_minutes`: minutes after T, default `baseline_minutes`, clamped to 30..120 and capped at now.

## Returns

- `verdict`: exactly one of `regression likely`, `no significant change`, `insufficient data`, with a one-line `reason`. `no significant change` is a valid answer, not a failure: report the change as safe.
- `golden_signals`: throughput (rpm), error rate (%, 5xx and gRPC errors) and p50/p95/p99 latency (ms; with no `env`, the slowest environment's) as baseline, after and `delta_pct`, for the service and its top 10 direct callers by throughput over both windows together, so a caller that stopped calling after T still shows (`dependents_dropped` counts the rest).
- `deviations`: `get_apm_service_deviations` results and, when `env` is set, `get_trace_attribute_deviations` over at most 15 minutes each side; 10 per list, `dropped` counts the rest. `deviations.apm.unavailable` lists the APM queries that failed while others succeeded.
- `alerts`: alerts for the service or its callers that started firing after T, matched case-insensitively by alert group name or a `service`, `service_name` or `service.name` label; 10 per list. With `env`, alerts labelled with another environment, ignoring case, are dropped and `env_match` is `exact`, or `unlabelled` when the alert carries no env label.
- `drill_down`: the next tools to call, with arguments, limited to tools this server serves.
- A failing upstream call leaves an `error` on its section; the other sections still return, and the verdict names what failed.

## Significance

`regression likely` when any holds: error rate up at least 1 percentage point, p95 up at least 25%, or throughput (rpm) down at least 50% for the service or a caller (only with at least 20 requests in each window); a new firing alert; a regression from `get_apm_service_deviations`. `insufficient data` when the after window is under 5 minutes (including T in the future), the service has no or too few requests, or golden signals, the direct-caller lookup, alerts or `get_apm_service_deviations` failed, wholly or in any query listed in `deviations.apm.unavailable`: missing evidence never yields `no significant change`, and a regression found anyway names what failed. Trace-attribute deviations explain a verdict but never set one, so skipping them or failing to fetch them leaves it unchanged.
