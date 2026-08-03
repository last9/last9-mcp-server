Workflow: exception root-cause investigation.

Use this workflow when investigating server-side exceptions, especially when
span errors may be a downstream symptom rather than the root cause.

Start with `get_exceptions` and follow the investigation flow documented in its
tool description end-to-end — that description is the single source of truth for
this flow. In short: identify the service and exception type, inspect
representative traces with `get_service_traces`, then decide whether the
exceptions are the answer or a symptom. For a log-heavy or severity-less
service, do not stop at the span exceptions — continue into logs (aggregate
first to isolate the hot logger, then read that logger's lines with a `limit`)
and report the actual root-cause error text.
