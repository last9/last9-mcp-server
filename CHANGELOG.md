# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Fixed

- `get_traces` no longer chunks `aggregate`/`window_aggregate` pipelines — long-window group-by queries run as a single request, fixing duplicate keys and wrong `avg`/`median`/`quantile` math (#195).
- Trace filter existence checks: `$exists` and `$notnull` are rewritten to `{"$neq": [field, ""]}` before hitting the backend (previously matched all spans / no spans respectively) (#195).

### Changed

- `get_traces` filter schema drops `$exists`/`$notnull` in favor of the `{"$neq": [field, ""]}` idiom; trace-query 408s now return a "narrow the window" error (#195).

## [0.13.0] - 2026-07-22

### Changed

- `mcp-server` Docker image is now multi-arch (`linux/amd64` + `linux/arm64`), built with `docker buildx` and pushed as a manifest list. Lets the image run natively on arm64/Graviton nodes without an amd64 node-selector pin (#192).

## [0.12.0] - 2026-07-17

### Added

- `get_apm_service_deviations` MCP tool comparing a current window against an equal-duration baseline (fleet or single service). Returns `regressions`/`improvements` leaderboards, `evidence_quality`, an Apdex reconciliation, and a terminal `outcome`. Arithmetic is computed server-side; correlations are supporting evidence only. V1 supports server-request workloads (#184).
- `list_dashboard_snapshots`, `get_dashboard_snapshot`, and `delete_dashboard_snapshot` MCP tools for frozen point-in-time dashboard snapshots. `list` returns snapshot metadata for a dashboard (`id`, `name`, `expires_at`, …); `get` returns the full frozen snapshot including `dashboard_definition`, `panel_data`, `time_range`, and `variables`; `delete` removes a snapshot by ID (#185).

## [0.11.0] - 2026-07-10

### Added

- Aggregate/count `get_logs` responses now carry an `l9_sanity` block (`matched_count`, `service_log_volume`, `ratio`) comparing the matched count against the service's total log volume in the same window (via `physical_index_service_count`). A count that is a large fraction of ALL of a service's lines flags the filter as too broad — e.g. counting a component/logger name without an `ERROR` gate. Informational only: it never blocks, never alters results, and is silently skipped on any failure (#180).

### Changed

- `get_log_attributes_for_pipeline` now discovers Body fields on non-JSON logs. Field discovery previously only `json.Unmarshal`'d Body lines, so plaintext/logfmt services surfaced zero body-derived fields even though `level` and other fields were present. The JSON path is unchanged and tried first; non-JSON lines now fall back to logfmt `key=value` extraction, then to well-known inline patterns (severity/level token, optional timestamp anchor, logger/class), each surfaced with a ready-to-use parse hint naming the correct parser (`logfmt`/`regexp`). Conservative — nothing is fabricated when no structure matches; indexed severity still wins name collisions (#181).
- Pipeline-validation `400`s from the log API now return a self-healing error — the stage schema reminder plus a pointer to `get_log_attributes_for_pipeline` — instead of a bare status, so a client that sent a malformed pipeline learns the fix in-band. The original error body is preserved (#180).
- `get_exceptions` description rewritten from an unconditional "STOP — do not call log tools" to a regime conditional: trace-instrumented services still stop (retains timeout protection against broad raw log pulls), while log-heavy/severity-less services continue to logs via aggregate/count pipelines where the root cause actually lives. Regime discriminator is `physical_index_service_count` log presence (#179).
- `get_logs` description gained a CRITICAL incident-investigation block: discover error signatures via an `ERROR`-gated group-by (never guess strings), never count a bare component/logger name without the `ERROR` gate, trend/onset via `window_aggregate`, sanity-check counts against the service's APM error rate, and fetch raw exemplars last. The `ERROR` gate spans `severity in (ERROR, FATAL, CRITICAL)` so fatal/critical rows aren't dropped. Description changes are eval-tested in last9-mcp-evals (#179).

## [0.10.0] - 2026-07-06

### Changed

- The HTTP (Streamable HTTP) server now runs the MCP handler in **stateless** mode. Session state was previously held per-instance in memory, so running more than one replica behind a load balancer caused intermittent `404 "session not found"` when a follow-up request (`tools/list`, `tools/call`) was routed to a different instance than the one that handled `initialize` — surfacing in clients as "tools fetch failed / no capabilities" plus reconnect storms. Stateless mode lets any instance serve any request, enabling safe horizontal scaling. Transport-contract change: the server no longer validates the `Mcp-Session-Id` header, and `GET /mcp` (the server→client SSE notification stream) now returns `405`. All tools are independent request/response queries and use neither server-initiated notifications nor session-scoped state (#174).
- Normalized MCP tool parameter names to canonical spellings so agents guess them correctly more often: `get_service_logs`, `get_service_environments`, and `get_change_events` now take `service_name` (was `service`); `get_change_events` and `get_exceptions` now take `env` (was `environment` / `deployment_environment`). The old spellings are removed — a call using them returns a recoverable `isError` rather than silently failing. `prometheus_labels` / `prometheus_label_values` additionally accept `match` as an alias of `match_query` (#176).
- Bumped `golang.org/x/net` 0.52.0 → 0.55.0 (#175).

### Fixed

- `get_databases` and `get_database_queries` reported database latency 1000x too high. Their PromQL queries multiplied `trace_client_duration` by 1000 on the assumption it was in seconds, but the metric is already in milliseconds — so `p95_latency_ms` / `avg_latency_ms` were inflated by three orders of magnitude (e.g. a real ~30s Redis blocking read surfaced as `p95_latency_ms: 30010484`, ~8.3 hours). Removed the multiplier; the values now match their `_ms` unit and the frontend's own database queries. Consumers that anchored dashboards or thresholds on the old inflated numbers will see values drop 1000x (#177).
- Malformed tool-call input (unknown parameter name, wrong value type) now surfaces to the model as a tool-call error (`CallToolResult.isError`) instead of a swallowed JSON-RPC `-32602` protocol error, letting the agent self-correct instead of burning the call. Achieved by bumping `modelcontextprotocol/go-sdk` v1.4.1 → v1.5.0, which adopts the SEP-1303 input-validation contract (#173).
- Single-condition trace filters in `get_traces` are now always wrapped in the `$and` logical operator that the tracejson spec requires. A bare top-level condition like `{"$eq": ["SpanKind", "SPAN_KIND_INTERNAL"]}` was forwarded unwrapped and rejected by the API; the server now normalizes one or more bare top-level field operators to `{"$and": [...]}` (keys sorted for deterministic output), while leaving a query already wrapped in `$and` / `$or` / `$not` untouched. The `get_traces` description's Example 7 was also corrected to model the wrapped form so weaker models emit valid queries directly (#172).
- `get_alerts` now instructs the model to cap `window` at its 3600-second (1-hour) maximum when the user asks for a longer period, instead of emitting the raw computed value (e.g. `5400` for "90 minutes", `7200` for "2 hours") which the server hard-rejects with `window must be between 1 and 3600 seconds` — turning a valid intent into an error. Description-only change (#171).

## [0.9.0] - 2026-06-25

### Added

- `get_trace_attributes_for_pipeline` tool for pipeline-scoped trace-attribute discovery. Given an in-progress pipeline (e.g. a `ServiceName` filter), it returns only the trace attributes actually present for that scope via `/cat/api/traces/v2/series/json`, each enriched with the exact `filter_field` to use in a `get_traces` condition. This prevents filtering on an attribute key that is empty for the queried scope (e.g. assuming `http.status_code` when the service uses `http.response.status_code`), which silently returns 0 — the trace-side counterpart of `get_log_attributes_for_pipeline` (#166).
- `dump-tools` subcommand prints the served `tools/list` result (`{"tools": [...]}`, sorted by name) by round-tripping a real request over in-memory transports, with no refresh token, credentials, or network needed. Output matches what clients receive (including `inputSchema` and annotations), making it a deterministic, credential-free tool snapshot for the eval harness and docs tooling (#164).
- `get_log_attributes_for_pipeline` now surfaces fields that exist only inside a JSON log Body (e.g. `uri` on access logs) by sampling raw rows for the scoped pipeline, reporting them as `source=body` entries with a `sample_coverage` ratio and a ready-made two-stage parse hint. Indexed attributes win name collisions; sampling failures degrade to the indexed-only response (#163).

### Changed

- `get_trace_attributes` (global catalog) now sources attributes from the trace tag catalog (`/cat/api/search/tags`) instead of an empty-pipeline series call, so it returns the full global attribute set rather than a subset. Output shape is unchanged (#166).
- `get_trace_attribute_values` now accepts an optional `pipeline` to scope the returned values to a filtered slice of spans; omit it for global values (#166).
- All tool description text is now embedded markdown under `internal/prompts/descriptions/` via `go:embed`, standardizing the single source of truth for descriptions across every tool (#165).
- Log tool descriptions now mandate attribute discovery before filtering in `get_logs`, with explicit discover-then-filter examples, and teach parse-then-group, service-variant enumeration, and the severity trap for Body-derived discovery (#155, #163, #167).

## [0.8.1] - 2026-06-18

### Fixed

- Dashboard create and update tools now expose `dashboard` and `metadata` inputs as JSON objects in their MCP schemas, allowing clients to pass dashboard definitions directly.

## [0.8.0] - 2026-06-08

### Added

- `get_log_attributes_for_pipeline` tool for pipeline-scoped log-attribute discovery. Given an in-progress pipeline (e.g. a `ServiceName` filter), it returns only the log fields actually present for that scope via `/logs/api/v2/series/json`, each enriched with the exact `filter_field` to use in a `get_logs` condition. This prevents filtering on an attribute key that is empty for the queried service (e.g. assuming `http.status_code` when the service uses `status_code`), which silently returned 0 and caused large undercounts (#160).
- `get_alert_rule_state` tool for historical firing state (1/0) per alert rule over a time range, grouped by `rule_id`. Supports server-side filtering by `alert_group_id`, `rule_name`, `alert_group_name`, `label_filters`, and `state` (#159).

## [0.7.5] - 2026-06-01

### Fixed

- Log attribute discovery now always uses `/v1/labels`, and the environment filter uses the correct key (#156).

## [0.7.4] - 2026-05-27

### Added

- `get_trace_attribute_values` tool for retrieving values of trace attributes (#150).

### Changed

- Tool calls now run as parallel chunked requests with adaptive parallelism and chunk sizing (#151).
- `get_service_logs` now exposes its enhanced description (#152).

### Fixed

- Corrected traces/logs sanitizer behavior (#150).

## [0.7.3] - 2026-05-25

### Changed

- `get_alert_config` now resolves each referenced indicator's PromQL query and unit inline, embedding them directly in the response. KPI lookup failures surface as inline notes rather than failing the entire request (#148).

## [0.7.2] - 2026-05-20

### Added

- MCP tools for dashboard CRUD: `list_dashboards`, `get_dashboard`, `create_dashboard`, `update_dashboard`, `delete_dashboard`.

## [0.7.1] - 2026-05-05

### Changed

- Default API host now derived from access token `aud` claim instead of hardcoded `app.last9.io`. Set `LAST9_API_HOST` to override.
- Drop rule endpoints now use the unified API host (previously routed to token `aud` host independently).
- `cfg.ActionURL` field retained for struct compatibility but no longer consumed internally.

### Fixed

- Startup `failed to refresh access token: ... 400 Bad Request` when token `aud` host did not match the hardcoded default. Error message also corrected to `failed to populate API config`.

## [0.7.0] - 2026-04-28

### Added

- `service.version` tracking and tenant identity across all deployments (#137).
- Trace query reliability: input validation, structured schema, and service env filter (#123).

### Fixed

- PromQL range/labels queries now anchor on end time, not start time (#138).
- `get_service_environments` filter uses correct `service_name` label (#133).
- `add_drop_rule` HTTP request now includes context (#134).
- `get_logs` rejects non-canonical filter shapes instead of silently dropping them (#131).

### Changed

- Upgraded `mcp-go-sdk` to v0.1.2 (#135).
- Bumped OpenTelemetry log exporter dependencies (#136).
- Automated `@last9/mcp-server` npm publish via OIDC trusted publishing (#139).

## [0.6.0] - 2026-04-19

### Added

- Per-query datasource selection for Prometheus tools (#129).

### Fixed

- Exception investigation now calls `get_service_traces` instead of `get_traces` (#122).

### Changed

- Rewrote README to cover all current tools with a cleaner structure (#128).

## [0.5.1] - 2026-03-02

### Fixed

- Use `[]map[string]interface{}` for `logjson_query` and `tracejson_query` schema (#93).

## [0.5.0] - 2026-02-28

### Added

- Max response time metric support in APM tools (#76).
- Increased max lookback window from 24h to 14 days.

### Fixed

- Correct curl testing examples to use MCP session handshake.
- Docs for hosted MCP, token type, Windows binary; telemetry disabled by default (#86).
- Note Claude Desktop does not support hosted HTTP MCP yet; revert to STDIO.

### Changed

- Bumped `go.opentelemetry.io/otel/sdk` to 1.40.0 (#91).
- Bumped `github.com/modelcontextprotocol/go-sdk` (#90).
- Updated README for v0.5.0 release (#92).

## [0.4.0] - 2026-02-17

### Added

- Deep link generation across handlers (dashboards, exceptions, service logs, AI assistant tools).
- Cluster parameter in dashboard deep links.

### Fixed

- Broken MCP tools (#73).
- Exception filter simplification and nested response handling.
- Exception attribute detection in traces.
- URL parameter escaping in deep link generation.
- Reference URLs for MCP tools.
- Match env label with regex in PromQL queries.
- Clarified `lookback_minutes` and time parameter defaults in docs (#74).

### Changed

- Improved tool descriptions (#78).
- Reverted "disable mutating tools by default until RBAC" — re-enabled.

## [0.3.0] - 2026-01-12

### Added

- Trace tools (#60).
- Refresh token support (#61).

### Changed

- Simplified authentication and cleanup (#64).
- Removed `and` condition from `get_trace_attributes`.

## [0.2.0] - 2025-11-03

### Added

- `get_traces` tool with trace ID and service name support (#58).
- `service_name` and `deployment_environment` filters in exceptions tool (#57).
- Docker image build for release branches (#54).
- Migration to official MCP SDK with telemetry (#46).

### Fixed

- Empty response from queries (#53, #55).

### Changed

- Tool improvements (#51).

[0.7.5]: https://github.com/last9/last9-mcp-server/compare/v0.7.4...v0.7.5
[0.7.4]: https://github.com/last9/last9-mcp-server/compare/v0.7.3...v0.7.4
[0.7.3]: https://github.com/last9/last9-mcp-server/compare/v0.7.2...v0.7.3
[0.7.2]: https://github.com/last9/last9-mcp-server/compare/v0.7.1...v0.7.2
[0.7.1]: https://github.com/last9/last9-mcp-server/compare/v0.7.0...v0.7.1
[0.7.0]: https://github.com/last9/last9-mcp-server/compare/v0.6.0...v0.7.0
[0.6.0]: https://github.com/last9/last9-mcp-server/compare/v0.5.1...v0.6.0
[0.5.1]: https://github.com/last9/last9-mcp-server/compare/v0.5.0...v0.5.1
[0.5.0]: https://github.com/last9/last9-mcp-server/compare/v0.4.0...v0.5.0
[0.4.0]: https://github.com/last9/last9-mcp-server/compare/v0.3.0...v0.4.0
[0.3.0]: https://github.com/last9/last9-mcp-server/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/last9/last9-mcp-server/compare/v0.1.15...v0.2.0
