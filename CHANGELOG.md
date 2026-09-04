# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- `last9://reference/investigation`: an MCP resource documenting the profile-first investigation flow.
- `get_service_profile` returns a per-service telemetry profile — signal presence, language/runtime, deployment envs, log `signal_shape`, and a recommended ingest fix — as a short brief followed by raw JSON. Call it before a service-scoped investigation to skip trace tools when traces are absent and to parse severity from the log body when `severity_set` is `none` or `partial`.

### Changed

- `get_service_performance_details` now returns about 200 data points per series instead of one per minute, and sizes each range selector from the resulting step, so a wide window aggregates the whole window instead of sampling part of it. Response-time values shift slightly, since that query's lookback is no longer fixed at 5m. Requires a Last9 API version that substitutes `$__rate_interval` on the range-query endpoint; against an older version the time-series fields come back empty with `partial_errors`.
- The service workflows and the service-scoped tool descriptions now call `get_service_profile` first: skip trace tools when the profile reports `telemetry.traces` as `absent`, and parse the level from the log body when `severity_set` is `none` or `partial`.

## [0.16.0] - 2026-08-27

### Added

- `get_logs` and `get_service_logs` can answer a search with one server-side call instead of the client-side chunk sweep. Set `LAST9_USE_LOG_SEARCH_API=true` (or `--use_log_search_api`); off by default. Output shapes are unchanged. `get_logs` gains `total_matching_lines`, `logs_truncated`, `search_stats` and `volume_summary` on raw searches (aggregates get `search_stats` only); `get_service_logs` gains `total_matching_lines` and `search_stats`. `total_matching_lines` counts all matching lines, not the returned sample — but it is a floor, not a total, when `logs_truncated` is true or `search_stats.chunks_failed` is above zero.

## [0.15.2] - 2026-08-26

### Fixed

- `get_logs` now caps aggregate result rows and sets `l9_result.partial` when capped; log and trace tools reject malformed `$quantile` arguments before execution, and log percentiles reject unsafe numeric-field pipelines.
- `get_change_events`: the `service_name`, `env`, and `event_name` filters now match the labels change events are actually stored with. A filtered call previously returned nothing while the same call without filters showed the events.
- `get_log_attributes_for_pipeline` now reports a plain-text Body's shape. A Body that is neither JSON nor logfmt and carries no severity token previously produced no Body entry at all, so models guessed `parser:"json"` and got a silent zero, or wrote an unanchored regexp that captured the leading timestamp and returned a confidently wrong count. Such services now get a `Body` entry with `source:"body"`, up to three `sample_bodies` lines to anchor a pattern against, and a one-stage `$regex`-on-Body hint. `sample_bodies` redacts URLs and known credential values only — it is not PII-redacted, so treat it as untrusted log content.
- `l9_sanity` is no longer suppressed when `matched_count` is 0 — previously the most suspicious outcome was the one case with no guardrail. A zero from a pipeline that parses or filters `Body` now carries a note pointing at `sample_bodies`, and a new `service_log_volume` key separates a genuine zero from a parse/filter mismatch. Zero counts from pipelines that never touch `Body`, and all non-zero paths, are unchanged.
- `get_service_performance_details` rejects windows over 366 days with a clear error instead of the previous integer-day-truncated message, which could self-contradictorily report "got 366 days, max is 366 days" right at the rejection boundary. Windows over 35 days are now split into 35-day-or-narrower chunks and merged (query cost was previously unbounded for very wide windows); a chunk's PromQL selector could also render as the invalid `[0m]` for a narrow trailing chunk, which is now clamped to a 1-minute minimum. Chunk boundary merging is now robust to adjacent chunks landing on different-resolution output grids; the same dedup also defensively drops any duplicate/non-monotonic timestamp within a single response, not just at chunk boundaries, so a plain (unchunked) window's merge is filtered the same way. Chunked sub-queries now run in parallel (bounded concurrency); a genuinely chunked (>35 day) call applies a per-chunk timeout, while a plain (<=35 day) call runs under the caller's own context with no added timeout, matching pre-chunking behavior exactly. Note: on a wide window, counter-style fields (throughput, error rate) come back on a dense time grid with explicit zeros for intervals that had no traffic, so a low-traffic service shows long runs of zeros rather than missing points — a low proportion of non-zero points is normal, not a sign of a failed query. Quantile-style fields (response times) only cover the intervals that actually had samples, so they can span far less than the requested window.

### Changed

- `get_service_performance_details` sub-query failures on a plain (unchunked, <=35 day) window: a non-2xx upstream status response stays soft (`partial_errors`, rest of the data still returned), and a read or parse failure on the response still hard-aborts the whole call — both matching pre-chunking behavior exactly. Both failure kinds stay soft for a genuinely wide window that got split into multiple chunks.
- `get_service_performance_details` accepts an optional `top_n` arg (default 10, previously fixed at 10, capped at 100) applied uniformly to `top_operations_by_response_time`, `top_operations_by_error_rate`, and `top_errors`. The latter two were previously unbounded for windows <=35 days — a slightly wider window could silently change from "every matching row" to "top 10". Requests above 100 now clamp to 100 instead of fanning out an unbounded backend `topk()`.

## [0.15.1] - 2026-08-16

### Fixed

- Log-query `400`/`422` responses now use the shared upstream sanitizer (URL/credential redaction, 512-byte truncation with `… (truncated)`) instead of echoing the raw body. `get_logs` / `get_service_logs` also append the pipeline schema hint pointing at `get_log_attributes_for_pipeline` (#213).
- `get_logs` fail-closed logjson validation with self-correcting tips (wrong keys/types, bare fields, NOT-SQL, bad parse/`window_aggregate` shapes); whale description aligned with API `window_aggregate`; missing parse `field` defaults to `Body`; `TraceId`/`SpanId`/`ParentSpanId` allowed for log↔trace correlation; dotted parse labels accepted; `tracejson.md` lookback default corrected to 60 (#212).

### Changed

- **Breaking:** a failed later time-chunk on `get_logs`, `get_service_logs`, or `get_traces` is a tool error. The previous `partial_result` / `_last9_mcp` merge-and-continue envelope is gone; clients that treated a truncated merge as success must handle the error. Declared truncation (`get_trace_waterfall` evidence, `get_apm_service_deviations` `partial_errors`) is unchanged (#213).
- `get_service_logs` accepts `http_status_class`, `http_status_code`, optional `http_status_field`, and `attribute_filters`, and echoes the resolved status field as `http_status_field`. Known-service HTTP status search should use this tool, not `get_logs` (#213).
- `get_service_performance_details` now records per-section PromQL HTTP failures in a new `partial_errors` field and still returns surviving metrics. Transport errors still fail the whole tool (#213).
- `get_logs` InputSchema uses a permissive stage `anyOf` (plus catch-all) so handler validation tips reach the model instead of opaque SDK `anyOf` errors (#212).
- **Breaking:** `get_service_summary` response shape is now a ranked `{rows: [...]}` envelope with snake_case fields (`request_count`, `throughput_rpm`, `http_4xx_count`, `http_5xx_count`, `grpc_error_count`) instead of a `map[string]ServiceSummary` with PascalCase keys (`ErrorRate`, etc.). Rows are `(service, env)` pairs sorted and limited server-side; clients that unmarshal the old map shape will fail and should be updated (#210).

## [0.15.0] - 2026-08-10

### Fixed

- `get_database_slow_queries` trace filters now use bracket map syntax — `db_system`, `host` and `env` previously returned a 400 from the traces API. The trace pipeline sanitizer also rewrites legacy `events.`, `resources.` and `resource.` dot notation (#202).
- `add_drop_rule` and `get_drop_rules` use `/otel_settings/drop` instead of the legacy `logs_settings/routing` path, which the API rejects with 405 (#201).

### Added

- MCP **workflow prompts** on `prompts/list` / `prompts/get`: six structured investigation templates — `scoped-log-attribute-discovery`, `exception-root-cause-investigation`, `investigate-latency-spike`, `diagnose-error-rate`, `analyze-slow-queries`, and `on-call-runbook`. The last four take arguments to scope the investigation. New `dump-prompts` subcommand lists the served prompts and their argument schemas (#183).

## [0.14.0] - 2026-08-03

### Added

- `get_trace_waterfall` MCP tool returning one exact trace as a bounded parent/child waterfall: millisecond timing, interval-union self-time, slowest spans, largest self-time contributors, and optional selected-span attributes/events/links. Truncation, cycles, duplicate spans, orphans and unparseable timestamps are reported as warnings with a matching `evidence_quality`; an empty result is `insufficient`, not a negative finding. No critical path is computed or claimed (#186).
- `get_trace_attribute_deviations` MCP tool ranking attribute values that differ between two bounded span cohorts — slow vs fast, error vs non-error, or two equal-duration windows. Returns full-denominator shares, percentage-point deltas, and representative trace IDs; results describe correlation, not cause. Requires the companion trace-analysis capability to be enabled (#186).
- `get_alert_config` server-side notification-channel filters matching Alert Studio dashboard semantics: `only_without_notification_channel` (dashboard "Not configured"), `notification_channel_types`, `notification_channel_names`, and `notification_channel_severities` (breach/threat on the same binding row). Every rule now includes `Notification Channels` and `Notification Channel Bindings` lines aligned with the rules-table column; alert group `name`, `data_source`, and `tags` are included when resolved (#191).
- `get_notification_channels` TSV output now includes `service_fqid`, the per-entity alert-group binding id (#191).
- Named **toolsets** that hard-filter which tools appear in `tools/list`, selected via `--toolsets` or `LAST9_TOOLSETS` (alias `LAST9_MCP_TOOLSETS`). Valid names are `logs`, `traces`, `metrics`, `alerts`, `dashboards`, `investigate`, and `all`; the value is comma-separated, and unset/empty/`all` serves the full surface as before. Unknown names fail fast at startup with the valid list rather than silently falling back. Lets an operator stop paying description tokens for tools a client never calls — an automation host pinned to `investigate` loads a fraction of the full surface. `dump-tools` honors the same flag and env var (#189).
- MCP **reference resources** under `last9://reference/*` (`logjson`, `tracejson`, `service_logs`, `metrics`) carrying the full query manuals for the large-description tools. Clients fetch them on demand via `resources/list` / `resources/read` (#189).

### Fixed

- `get_apm_service_deviations` terminal outcomes (`stable`, `no_data`, `unsupported_workload_shape`) now all return an empty `recommended_followups`, so agents do not keep calling follow-up tools after a completed comparison. Previously `unsupported_workload_shape` returned a `get_service_traces` follow-up that contradicted the description's stop rule (#197).
- Exception→logs guidance in `get_exceptions` is now aggregate-then-read: aggregate to isolate the hot logger, then read that logger's lines with a `limit` and report the error text. Raw line fetches were previously banned outright in this flow, leaving no way to reach the log body the root cause lives in — the hazard is an unlimited fetch, not reading lines. `get_logs` and `get_service_logs` keep their own descriptions; the investigation flow lives only in `get_exceptions` (#197).
- `get_notification_channels` / `get_alert_config` channel binding fetches use `?exact=true` on `/notification_settings` so per-entity mapped channels load (without it, only global/master rows returned and binding filters falsely reported every rule as unconfigured) (#191).
- `get_traces` no longer chunks `aggregate`/`window_aggregate` pipelines — long-window group-by queries run as a single request, fixing duplicate keys and wrong `avg`/`median`/`quantile` math (#195).
- Trace filter existence checks: `$exists` and `$notnull` are rewritten to `{"$neq": [field, ""]}` before hitting the backend (previously matched all spans / no spans respectively) (#195).

### Changed

- Trace tools (`get_traces`, `get_service_traces`, `get_trace_attributes`, `get_trace_attribute_values`, `get_trace_attributes_for_pipeline`, `get_exceptions`) now return recoverable tool errors (`isError: true`) with sanitized messages instead of JSON-RPC protocol errors when upstream trace calls fail. Upstream response bodies, URLs, and credentials are no longer echoed into model context; `400`/`422` responses still include the upstream rejection text, and the calls that carry a caller-supplied pipeline also get a schema hint pointing at `get_trace_attributes_for_pipeline` (#188).
- Progressive disclosure for the large-description tools (`get_logs`, `get_traces`, `get_service_logs`, `prometheus_range_query`): each now serves a short description carrying the firing blurb plus the critical query-construction rules, and points at its `last9://reference/...` resource for the full manual. Behavior change for clients that scraped the whole query manual out of `tools/list` — the rules needed to build a correct query stay on the tool, but the exhaustive DSL reference must now be read as a resource (#189).
- Org attribute catalogs are no longer injected into served tool descriptions. Descriptions point at the discovery tools (`get_log_attributes_for_pipeline`, `get_trace_attributes_for_pipeline`) instead, so the served surface no longer varies by org and models stop over-anchoring on a snapshot of attribute names (#189).
- `get_traces` filter schema drops `$exists`/`$notnull` in favor of the `{"$neq": [field, ""]}` idiom; trace-query 408s now return a "narrow the window" error (#195).
- Bumped `google.golang.org/grpc` 1.80.0 → 1.82.1 (#194).

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
