# last9-mcp-server

The MCP server exposing Last9 observability (metrics, logs, traces, alerts, dashboards) as tools an LLM agent can call. This glossary fixes the **canonical parameter vocabulary** across tools so the same concept is always spelled the same way.

## Language — tool parameter names

**service_name**:
The service a query is scoped to (e.g. `checkout-service`). The single canonical spelling for the service concept across all tools.
_Avoid_: service, svc

**env**:
The deployment environment a query is scoped to (e.g. `prod`, `staging`). The single canonical spelling for the environment concept.
_Avoid_: environment, deployment_environment

**match_query**:
A PromQL series selector used by the label-discovery tools (`prometheus_labels`, `prometheus_label_values`) to choose which series to inspect. Distinct from `query`.
_Avoid_: (none — but `match` is an accepted **alias** on the label tools, absorbing the Prometheus `match[]` ecosystem convention)

**query**:
An executable PromQL expression that returns data (`prometheus_instant_query`, `prometheus_range_query`). A different concept from `match_query` — do not conflate the two.

## Notes

- **Canonical-choice rule:** favor the existing majority spelling. Where multiple spellings exist, pick one canonical name and treat the others as deprecated (removed) — not silently coerced.
- **Alias policy:** we do **not** keep backward-compat aliases (recovery via `isError` handles callers of removed names). The sole exception is an **ecosystem prior** a rename cannot erase (currently only `match`).
