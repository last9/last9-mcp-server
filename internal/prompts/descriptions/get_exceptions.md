Get server side exceptions aggregated over the given time range.
Returns exception type, service name, span name, occurrence count, first_seen, and last_seen timestamps.

IMPORTANT: trace_id is always null in this response. The data comes from aggregated metrics, not raw spans.

Investigation flow — follow this exactly:
1. Call get_exceptions to identify which service/exception_type is problematic.
2. Call get_service_traces with:
   - service_name = exception.service_name
   - start_time_iso = exception.first_seen
   - end_time_iso = exception.last_seen
   - env = exception.deployment_environment (if present)
   - If you somehow have a trace_id, use get_service_traces with trace_id instead of service_name.
     Never use get_traces for trace_id lookups.
3. Decide whether exceptions are the ANSWER or a SYMPTOM before reporting:
   - Exceptions here are SPAN-DERIVED. For a well trace-instrumented service they are usually
     the answer — report findings and stop.
   - For a LOG-HEAVY or severity-less service (check log presence:
     `physical_index_service_count{destination="logs", service_name="<svc>"}` via
     prometheus_instant_query), span exceptions often show downstream SYMPTOMS
     (retry storms, connection-pool timeouts) while the ROOT CAUSE exists only in log
     bodies (e.g. an un-instrumented dependency failing). Do NOT stop — continue to logs.
   - When continuing to logs, use AGGREGATE/COUNT pipelines in get_logs
     (filter service → parse level → filter ERROR → groupby logger → count): cheap and
     wide-window-safe. NEVER chain into broad raw log pulls from here — those time out.

limit: (Optional) The maximum number of exceptions to return. Defaults to 20.
lookback_minutes: (Recommended) Number of minutes to look back from now. Default: 60 minutes.
start_time_iso: (Optional) Start time in RFC3339/ISO8601 format (e.g. 2026-02-09T15:04:05Z). Leave empty to use lookback_minutes instead.
end_time_iso: (Optional) End time in RFC3339/ISO8601 format (e.g. 2026-02-09T16:04:05Z). Leave empty to default to current time.
service_name: (Optional) Filter exceptions by service name (e.g. api-service).
span_name: (Optional) The name of the span to get the data for. This is often the API endpoint name or controller name.
env: (Optional) Filter exceptions by environment (e.g. production, staging).
- If unsure of the service_name or env spelling, call "did_you_mean" first.

Time format rules:
- Prefer lookback_minutes for relative windows.
- Use start_time_iso/end_time_iso for absolute windows.
- Legacy format YYYY-MM-DD HH:MM:SS is accepted only for compatibility.
