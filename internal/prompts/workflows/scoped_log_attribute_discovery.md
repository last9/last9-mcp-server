Workflow: scoped log attribute discovery before filtering.

Use this workflow for log questions about a specific service, especially HTTP
status, error rate, counts, breakdowns, or fields that may live in the log body.

Steps:
1. First call `get_log_attributes_for_pipeline` with a pipeline scoped to the
   requested `ServiceName` and time window.
2. After scoped discovery, go directly to `get_logs`. Do not inspect raw
   examples first; use the discovered field list, source, and parse hint to
   construct the aggregate query.
3. Build the final `get_logs` query only from fields that scoped discovery
   returned for that service. Do not guess common status keys such as
   `http.status_code`, `status_code`, `StatusCode`, or
   `http.response.status_code`.
4. If scoped discovery shows `source: "body"` or provides a parse hint, copy
   that parse stage before filtering or grouping on the derived field.
5. For HTTP 5xx access-log questions, do not use `SeverityText` or severity as
   a proxy. Access logs can be INFO even for 5xx responses.
6. Use `get_logs` for final counts, breakdowns, totals, and aggregates. Do not
   call `get_service_logs` for aggregate answers; it fetches raw samples and is
   not the answer path for this workflow.
7. Forbidden tool for this workflow: `get_service_logs`. Never call it in this
   workflow, even if a `get_logs` attempt fails or returns an unexpected shape.
   Fix or simplify the `get_logs` pipeline and retry `get_logs` instead.

The final query must be scoped to the target service and return the requested
count or grouped count, not a broad raw sample.
