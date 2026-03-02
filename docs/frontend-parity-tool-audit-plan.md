# Frontend-Parity Retrieval Program for MCP Tools (ENG-691 + Full Tool Surface)

## Summary
Bring all 21 MCP tools onto a frontend-parity retrieval path, starting with ENG-691 (`get_exceptions`) and expanding to every tool so request construction, datasource selection, query defaults, and endpoint usage mirror dashboard behavior.

Chosen defaults:
- Parity level: frontend fetch parity, while keeping current MCP output schema stable where possible.
- Rollout: phased migration behind a feature flag.

Primary frontend references:
- `/Users/rizwan/work/last9/dashboard/app/src/App/scenes/Exceptions/hooks/useFetchExceptions.ts`
- `/Users/rizwan/work/last9/dashboard/app/src/App/scenes/Traces/TracesApis.ts`
- `/Users/rizwan/work/last9/dashboard/app/src/App/scenes/Logs/LogsApis.ts`
- `/Users/rizwan/work/last9/dashboard/app/src/App/scenes/Levitate/api/ClustersApi.ts`
- `/Users/rizwan/work/last9/dashboard/app/src/App/scenes/ApiCatalog/queryConfigs.util.ts`

## Scope
- In scope: `last9-mcp-server` request-path parity for all registered tools, per-tool gap documentation, parity tests, phased rollout controls.
- Out of scope: frontend behavior changes, dashboard API contract changes, tool renaming/removal.

## Deliverables
1. Tool gap audit doc at `docs/frontend-parity-tool-audit.md` with one section per tool: current behavior, frontend behavior, gap, fix.
2. Parity request layer in MCP server (`internal/frontendparity/*`) to centralize endpoint/params/header/query composition.
3. Tool migrations to parity layer for all 21 tools.
4. Parity test suite (unit + integration/mocked HTTP) and ENG-691 regression coverage.
5. Feature-flagged rollout (`legacy`, `shadow`, `parity`) with observability on parity drift.

## Tool-by-Tool Differences and Suggested Fixes
| Tool | Current MCP retrieval | Frontend retrieval pattern | Key gap today | Suggested fix |
|---|---|---|---|---|
| `get_exceptions` | Traces JSON query on `attributes['exception.type']` | PromQL (`trace_*_count`) + optional last-seen range query | Fundamental mismatch; misses frontend-derived exception view | Rebuild using frontend exceptions PromQL path; keep MCP schema but derive data from same metrics path |
| `get_service_summary` | Custom 3 instant queries | Service catalog PromQL patterns | Error path differs (HTTP-only vs frontend HTTP+gRPC handling) | Port query builders from service-catalog logic and align filters/defaults |
| `get_service_environments` | `prom_label_values(env, domain_attributes_count...)` | Same endpoint family | Mostly aligned, but datasource selection is static | Route through parity datasource resolver |
| `get_service_performance_details` | Custom mixed instant/range queries | API Catalog query-config-driven fetches | Query drift and env matcher inconsistencies | Port query-config-based builders and normalize env handling |
| `get_service_operations_summary` | Custom instant queries | Operations hooks query groups/configs | Query drift + endpoint/server ops currently not appended in MCP output | Port operations query config and fix append regression |
| `get_service_dependency_graph` | Custom call-graph queries | Global topology/service dependency query style | Not sourced from same frontend query layer | Align with frontend call-graph query templates and grouping |
| `prometheus_range_query` | `/prom_query` | `/prom_query` | Missing frontend-style `step`/datasource-selection parity | Add optional `step`; use parity datasource resolver |
| `prometheus_instant_query` | `/prom_query_instant` | `/prom_query_instant` | Static datasource only | Resolve datasource like frontend-selected source |
| `prometheus_label_values` | `/prom_label_values` | `/prom_label_values` | Static datasource and limited normalization | Route via parity layer and preserve frontend param semantics |
| `prometheus_labels` | Calls `/apm/labels` | Frontend `prom_labels` for prom labels | Endpoint mismatch | Switch tool to `/prom_labels` parity path |
| `get_logs` | `/logs/api/v2/query_range/json` | Logs V2 query range JSON with richer params | `limit` ignored; no index/offset/mode parity | Add frontend-equivalent query params and honor `limit` |
| `get_service_logs` | Custom service filter + physical index heuristic | Logs query_range path with frontend filter semantics | Regex/operator/index behavior diverges | Rebuild using frontend logs query semantics; explicit index support |
| `get_drop_rules` | `logs_settings/routing` without region | Frontend includes `region` | Missing required routing context | Add `region` parity param handling |
| `add_drop_rule` | PUT routing without `region`/`cluster_id` parity | Frontend save includes `region` + `cluster_id` | Context mismatch; payload flow differs | Add `region` + `cluster_id`; match frontend save flow semantics |
| `get_alert_config` | GET `/alert-rules` | Same | Retrieval aligned; response formatting differs | Keep retrieval; optionally expose raw JSON mode while preserving current default schema |
| `get_alerts` | GET `/alerts/monitor` with timestamp/window | Same + optional `alert_group_id` filters | Missing alert-group filter parity | Add `alert_group_ids` passthrough |
| `get_traces` | Traces query_range with fixed defaults | Same endpoint with configurable order/direction/mode/span_limit | Missing parity params | Add order/direction/mode/span_limit parity args |
| `get_service_traces` | query_range for both trace_id and service_name | Frontend uses `/traces/{id}` for detail + query_range for search | Trace-id path mismatch; env key mismatch | Use `/traces/{id}` for trace-id mode; align env filter field naming |
| `get_log_attributes` | Duration-based switch v1 labels vs v2 series | Frontend chooses API by query mode/intention | Selection logic mismatch | Align to frontend mode-driven behavior and support index/pipeline filters |
| `get_trace_attributes` | v2 series with empty pipeline | Frontend can pass pipeline context | No pipeline parity | Add optional pipeline input and parity request shaping |
| `get_change_events` | Prom queries + event_type-based discovery every call | Frontend query patterns center around `last9_change_events` filters and event-name exclusions | Label/filter drift (`event_type` vs frontend usage) + extra discovery overhead | Align labels/filters to frontend conventions; make event-name discovery optional |

## Public API / Interface Changes (MCP tool args)
Additive-only, backward-compatible:
1. Common optional context fields where relevant: `datasource_name`, `cluster_id`, `region`.
2. Query controls: `step`, `order`, `direction`, `mode`, `span_limit`, `index`, `index_type` (tool-specific).
3. Alerts: `alert_group_ids`.
4. Attributes tools: optional `pipeline`.
5. Drop-rule tools: `region`, `cluster_id`.

No tool name changes. Existing fields remain supported.

## Implementation Plan (Phased)
1. Phase 1: Parity foundation
   - Build `internal/frontendparity` request builders for Prom, traces, logs, alerts, control-plane.
   - Add datasource resolver mirroring frontend source-selection model.
   - Add feature flag: `MCP_FRONTEND_PARITY_MODE=legacy|shadow|parity`.

2. Phase 2: ENG-691 + trace/log core
   - Migrate `get_exceptions`, `get_traces`, `get_service_traces`, `get_trace_attributes`, `get_logs`, `get_service_logs`, `get_log_attributes`.
   - Add ENG-691 regression tests with known-error fixtures.

3. Phase 3: Prom/APM tools
   - Migrate all `prometheus_*` tools and APM summary/details/operations/dependency tools to query-config parity.
   - Fix operations-summary missing append path as part of parity migration.

4. Phase 4: Alerting/change/drop tools
   - Migrate `get_alert_config`, `get_alerts`, `get_change_events`, `get_drop_rules`, `add_drop_rule` with parity params.

5. Phase 5: Shadow validation and cutover
   - Run shadow mode to compare legacy vs parity payloads/results.
   - Promote to `parity` default after acceptance thresholds; keep temporary fallback.

## Test Cases and Scenarios
1. Per-tool request-construction tests: endpoint, method, query params, and body match expected frontend-style payloads.
2. ENG-691 regression: known exception dataset returns non-empty exception output where legacy path returned empty/null exception fields.
3. Backward-compat contract tests: existing MCP response schemas remain parseable.
4. Datasource selection tests: `cluster_id`/`datasource_name` route to correct Prom read URL and credentials.
5. Edge cases: time-range precedence, env filters, empty results, multi-filter combinations, and high-cardinality limits.
6. Shadow-mode diff tests: legacy vs parity differences are surfaced and logged deterministically.

## Assumptions and Defaults
1. "Same way as frontend" applies to retrieval path and query semantics, not mandatory identical presentation formatting.
2. Existing MCP response shapes remain unless unavoidable; any unavoidable changes will be additive.
3. Dashboard is source of truth for endpoint/query defaults.
4. No frontend repo changes are required for this effort.
