Workflow: exception-to-log-root-cause continuation.

Use this workflow when investigating exceptions where span errors may be a
downstream symptom rather than the root cause.

Steps:
1. Use `get_exceptions` to identify the service, exception type, and time
   bounds.
2. Use `get_service_traces` only to inspect representative traces.
3. Continue into `get_logs` with an aggregate `logjson_query` over the same
   service and time window to rank error signatures or body-level failure
   messages.

Rules:
- Do not call `get_service_logs`; it fetches raw samples and can miss the
  ranked root-cause pattern.
- Use `get_logs` for log continuation.
- The `get_logs` query should include a service scope and an aggregate or
  `window_aggregate` stage before any raw-log drilldown.
- If severity may be empty or unreliable, aggregate on body-derived signatures
  or message patterns instead of relying only on `SeverityText`.
- Only answer once you have checked whether logs contain the root-cause signal
  behind the span exceptions.
