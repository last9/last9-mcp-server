Workflow: exception root-cause investigation.

Use this workflow when investigating server-side exceptions, especially when
span errors may be a downstream symptom rather than the root cause.

Steps:
1. Call `get_exceptions` to identify the problematic service and exception
   type, and the time bounds (`first_seen` / `last_seen`).
2. Call `get_service_profile(service_name=<service from step 1>)`. Use the
   result for all routing below — do not re-derive telemetry shape via PromQL
   or attribute probing.
3. Call `get_service_traces` (service_name, start/end from the exception
   window, env when present) to inspect representative traces.
4. Route using the profile from step 2:
   - telemetry.traces == "present" AND severity_set == "all"
     → exceptions are likely the answer; report and stop.
   - telemetry.logs == "present" AND (severity_set in ("none", "partial") OR telemetry.traces == "absent")
     → exceptions are likely symptoms; continue to logs (step 5).
   - derivation.log_tier == "failed" OR signal fields == "unknown"
     → proceed with caution; use get_log_attributes_for_pipeline as fallback.
   - Results contradict profile → apply contradiction clause.
5. Continue into logs, AGGREGATE FIRST: build the `get_logs` count pipeline
   using profile.signal_shape — use profile.level_field (not SeverityText when
   severity_set is none or partial); if log_format is "json", add parse stage
   per profile.parse_hint; gate on ERROR/FATAL/CRITICAL using the level field;
   aggregate `$count` grouped by logger. This is cheap and wide-window-safe.
   Never raw-fetch lines before this aggregate — an unnarrowed fetch times out
   over wide windows.
6. Once the aggregate isolates the hot logger, READ THE LINES: filter to that
   logger and always pass a `limit` (a handful of lines is enough). Use a
   non-aggregate `get_logs` pipeline for this bounded read — never
   `get_service_logs` during or after this investigation flow. Report the
   actual root-cause error text you read — the count locates the problem, the
   lines explain it.

When `error_detection.recommended_ingest_fix` is present in the profile,
include it in the investigation report.
