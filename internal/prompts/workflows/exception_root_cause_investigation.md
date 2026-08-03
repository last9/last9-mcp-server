Workflow: exception root-cause investigation.

Use this workflow when investigating server-side exceptions, especially when
span errors may be a downstream symptom rather than the root cause.

Steps:
1. Call `get_exceptions` to identify the problematic service and exception
   type, and the time bounds (`first_seen` / `last_seen`).
2. Call `get_service_traces` (service_name, start/end from the exception
   window, env when present) to inspect representative traces.
3. Decide whether the exceptions are the ANSWER or a SYMPTOM:
   - Span-derived exceptions are usually the answer for a well
     trace-instrumented service — report the findings and stop.
   - For a log-heavy or severity-less service, span exceptions often show
     downstream symptoms (retry storms, connection-pool timeouts) while the
     root cause lives only in log bodies. Do not stop — continue into logs.
4. Continue into logs, AGGREGATE FIRST: `get_logs` with a count pipeline
   (filter service -> parse `level` from Body if needed -> filter severity in
   (ERROR, FATAL, CRITICAL) -> aggregate `$count` grouped by logger). This is
   cheap and wide-window-safe. Never raw-fetch lines before this aggregate —
   an unnarrowed fetch times out over wide windows.
5. Once the aggregate isolates the hot logger, READ THE LINES: filter to that
   logger and always pass a `limit` (a handful of lines is enough). Use
   `get_service_logs` or a non-aggregate `get_logs` for this bounded read.
   Report the actual root-cause error text you read — the count locates the
   problem, the lines explain it.

If `SeverityText` is empty or unreliable, parse a `level` field from the Body
and gate on it instead of relying on severity alone.
