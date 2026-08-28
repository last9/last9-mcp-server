# Service-scoped investigation orchestration

Use this guide after calling `get_service_profile` for any service-scoped investigation (logs, traces, exceptions, latency, errors). The profile is derived once per service (~15 minute TTL); do not re-derive telemetry shape via PromQL or attribute probing when the profile is available.

## Bootstrap sequence (mandatory)

Run in order before any other service-scoped tool:

1. **`did_you_mean`** — only if the service name is uncertain.
2. **`get_service_profile(service_name)`** — always, before logs/traces/exceptions/performance tools.
3. **Pick `env`** from `profile.deployment.envs`; if multiple or empty, use the workflow parameter or call `get_service_environments` as fallback.

Never block the whole investigation on one failed probe — set affected fields to `unknown` and continue with fallback rules below.

## Tri-state semantics

Profile fields use `present`, `absent`, or `unknown`. **`unknown` means not measured — not the same as `absent`.**

| Field | `unknown` does NOT mean |
|---|---|
| `telemetry.traces` | traces are missing |
| `telemetry.logs` | logs are missing |
| `signal_shape.severity_set` | no severity coverage |
| `signal_shape.trace_context_in_logs` | no trace correlation in logs |
| `telemetry.metrics` | always `unknown` in v1 — do not use for routing |

When `derivation.log_tier == "failed"`, log-tier fields may be stale or empty — fall back to `get_log_attributes_for_pipeline`. When `derivation.log_tier == "skipped"` (empty region), expect a metrics-only profile.

## Severity routing

| `severity_set` | Agent behavior |
|---|---|
| `all` | `severity_filters` / `SeverityText` are reliable |
| `none` | Parse `level_field` from body; **never** `severity_filters` |
| `partial` | **Same as `none`** — severity is unreliable on most volume; body-parse is the safe default |
| `unknown` | Fall back to discovery; do not assert absence of severity |

When building log pipelines for `none` or `partial`, gate on ERROR/FATAL using the discovered `level_field` (not `SeverityText`). Copy `parse_hint` into the parse stage when `log_format == "json"`.

## Profile → routing rules

| Profile signal | Branch |
|---|---|
| `telemetry.traces != "absent"` | Trace path: `get_exceptions` → `get_service_traces` |
| `telemetry.traces == "absent"` | Skip trace tools; go to logs or stop |
| `telemetry.traces == "unknown"` | Not measured — take the trace path anyway; never treat as absent |
| `telemetry.logs != "absent"` + `severity_set` in (`none`, `partial`) | Parse `level_field` from body; never `severity_filters` |
| `telemetry.logs != "absent"` + `log_format == "json"` | `get_logs` with parse stage per `parse_hint` |
| `telemetry.logs == "absent"` | Don't chase logs; exceptions/traces are the answer |
| `deployment.envs` non-empty | Pick env from profile before `get_service_environments` |
| `derivation.log_tier == "failed"` | Warn; fall back to `get_log_attributes_for_pipeline` |
| `signal_shape` fully populated (`log_format != "unknown"`, `level_field` set) | Skip attribute discovery; go straight to scoped `get_logs` |
| Results contradict profile | Apply contradiction clause (below) |

### Exception root-cause routing (profile at step 2)

Call modality-specific tools unless `profile.telemetry` reports them absent — skip `get_service_traces` only when `telemetry.traces == "absent"`, and skip `get_logs` only when `telemetry.logs == "absent"`. `unknown` takes the tool path.

- `severity_set == "all"` AND `telemetry.traces != "absent"` → exceptions are likely the answer; report and stop. Fetching traces is a separate step and is not gated on `severity_set`.
- `telemetry.logs != "absent"` AND (`severity_set` in (`none`, `partial`) OR `telemetry.traces == "absent"`) → exceptions are likely symptoms; continue to logs.
- `derivation.log_tier == "failed"` OR signal fields == `unknown` → proceed with caution; use `get_log_attributes_for_pipeline` as fallback.

## Contradiction clause (trust but verify)

Follow the profile first. If the chosen branch returns empty results or contradicts symptoms (e.g. profile says `telemetry.logs: absent` but exceptions suggest log-body root cause), fall back to discovery tools (`get_log_attributes_for_pipeline`, `get_service_environments`) and note the profile may be stale (15min TTL) or a derivation tier may have failed. Trust fresh evidence over the profile when they conflict.

## `error_detection` — report only

When `error_detection.recommended_ingest_fix` is present, **include it in the investigation report** for the operator (ingest hygiene signal). Do **not** use it for routing — routing follows `severity_set`, `level_field`, and `telemetry.*` presence above.

## Fallback rules

| Condition | Fallback |
|---|---|
| `derivation.log_tier == "failed"` | `get_log_attributes_for_pipeline` scoped to the service |
| `severity_set == "unknown"` | Discover severity/level fields before filtering |
| `deployment.envs` empty or ambiguous | `get_service_environments` |
| `signal_shape.log_format == "unknown"` | Small scoped `get_logs` sample or `get_log_attributes_for_pipeline` — never broad high-volume pulls |
| Profile unavailable or probe failed | Run discovery probes; never guess field names or fabricate probe results |
| `telemetry.metrics` | Ignore in v1 (always `unknown`) |
| `dependencies` | Ignore in v1 (always null) |

**Never compensate for a failed probe with a broad/unscoped query** — high-volume datasources time out on wide pulls. Use narrow time windows and small limits when sampling is required.
