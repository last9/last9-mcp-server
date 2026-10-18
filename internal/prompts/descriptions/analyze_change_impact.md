Answers "did the change at T break anything?" by comparing one service before and after a change. Use it once `get_change_events` or the user names a change near a symptom, or to confirm one was safe.

## Parameters

- `change_event`: one element of `get_change_events` `change_events[]`, passed unchanged. Uses `metric.service`, `metric.deployment_environment` and the earliest `values[].timestamp`.
- `service` + `timestamp`: the alternative to `change_event`; `timestamp` is RFC3339 or unix seconds, rounded down to the minute. Pass exactly one form.
- `env`: exact environment (default: the event's, else all).
- `baseline_minutes`: minutes before T, default 60, clamped to 30..120.
- `after_minutes`: minutes after T, default `baseline_minutes`, clamped to 30..120 and capped at now.

## Returns

- `verdict`: exactly one of `regression likely`, `no significant change`, `insufficient data`, with a one-line `reason`. `no significant change` is a valid answer, not a failure: report the change as safe.
- `golden_signals`: throughput (rpm), error rate (%, 5xx and gRPC errors; `deviations.apm` also counts error-status spans and 429s) and p50/p95/p99 latency (ms; with no `env`, the busiest env's in both windows, `latency_env`) as baseline, after and `delta_pct`, for the service and its top 10 direct callers by throughput over both windows together, so a caller that stopped after T still shows (`dependents_dropped` counts the rest). A caller's signals are its own server spans: client-only ones show 0 rpm.
- `deviations`: `get_apm_service_deviations` results and, with `env`, `get_trace_attribute_deviations` over at most 15 minutes each side; 10 per list, `dropped` counts the rest. `deviations.apm.unavailable` lists failed APM queries.
- `alerts`: alerts for the service or its callers that started firing after T, matched in any case by alert group name or a `service`, `service_name` or `service.name` label, else annotation (never free text such as a summary); 10 per list. With `env`, alerts labelled with another env, in any case, are dropped; `env_match` is `exact`, or `unlabelled` without an env label.
- `drill_down`: next calls with arguments, served tools only.
- A failing upstream call leaves an `error` on its section only; the verdict names it.

## Significance

`regression likely` when any holds: error rate up at least 1 percentage point, p95 up at least 25%, or throughput (rpm) down at least 50% for the service or a caller (only with at least 100 requests before T and, but for throughput, after; p95 in `latency_env`); a new firing alert; a regression from `get_apm_service_deviations`. `insufficient data` when the after window is under 5 minutes (or T is in the future), the service has too few requests, or golden signals, the direct-caller lookup, alerts or `get_apm_service_deviations` failed, wholly or in any query listed in `deviations.apm.unavailable`: missing evidence never yields `no significant change`, and a regression found anyway names what failed. Trace-attribute deviations explain a verdict but never set one, even when skipped or failed.
