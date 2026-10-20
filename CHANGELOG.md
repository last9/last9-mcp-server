# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- `analyze_change_impact` returns `regressions[]`: under `regression likely`, one entry per trigger, in `reason`'s order, with `service`, `role`, `env` (the environment measured; `""` = all), `metric` (`error_rate_pct`, `latency_p95_ms`, `throughput_rpm`, `alert` or `apm_deviation`) and `baseline`/`after` (alerts add `rule_id` and `state`), all service-wide, so callers that file tickets need not parse `reason`; it is `[]` for the other verdicts. With no `env` argument, error-rate and throughput items sum every environment (`env: ""`) while p95 uses the busiest. An alert now sets `regression likely` only while still firing at the end of the after window; one that resolved stays in `alerts.started` and is counted in `reason` (#364).
- `get_metric_status` (`metrics` and `investigate` toolsets) reports whether a metric or series selector is live, its series count, the age of its newest sample, its inferred emission interval, and a suggested panel window (`$__interval` for series emitting at most every 5 minutes, else about 3x the larger of last-sample age and interval, e.g. `3d` for daily emitters). The interval comes from a raw-sample `scrape_interval` rollup, so it does not change with the lookback window. An absent metric returns `live: false` with `series_count: 0`; empty `metric` or `window_minutes` below 1 are tool errors, and so is an upstream reply with no payload, so a failed read is never reported as a dead metric (#316).
- `get_alert_config` and `get_entity_alert_rules` now show each rule's pause, mute and schedule state, so a `patch_alert` change can be read back: `Disabled` (the API's `is_disabled` when present, else `state == disabled`), `Muted until` (`none`, `indefinitely` or RFC3339 UTC), `Recurring mute schedule` (timezone, `enabled: false` when paused, one `Mon,Tue 22:00-06:00` line per window; legacy single-window schedules included) and `Next transition` when the API reports one (#355).
- `--read-only` (env `LAST9_MCP_READ_ONLY`) serves only tools annotated `readOnlyHint: true`, hiding every tool that creates, changes or deletes alert rules, notification channels, dashboards, snapshots or drop rules, or acknowledges, reassigns or mutes alerts; a tool without annotations is treated as a write tool. `--allow-write-tools=a,b` (env `LAST9_MCP_ALLOW_WRITE_TOOLS`) keeps named write tools. The server fails to start on a non-boolean `LAST9_MCP_READ_ONLY`, on an allowed name that is not a write tool, and on `--allow-write-tools` without `--read-only`. Combines with `--toolsets` (a tool must pass both), and applies to stdio, `--http` and `dump-tools`; the `initialize` instructions and each tool description's routing sections name only the tools actually served, no served tool's description or schema names a hidden write tool, and the `create_dashboard`, `describe_alert_chart`, `create_alert_from_chart` and `recommend_alert_config` results name a write tool only when it is served (#356).
- Added `analyze_change_impact` (metrics, alerts and investigate toolsets): given a change event or a service and time, it compares the windows before and after the change — golden signals for the service and its top direct callers, APM and trace-attribute deviations, alerts that started firing — and returns `regression likely`, `no significant change` or `insufficient data` with drill-down calls and a reason naming each threshold crossed (`p95 latency rose 41% … (threshold 40%)`). A failing upstream call degrades only its section, and a failed golden-signals, caller, alerts or APM-deviations section — including any single failed APM query, listed in `deviations.apm.unavailable` — turns a would-be `no significant change` into `insufficient data`; trace-attribute deviations only explain the verdict, so their failure does not (#352).
- Rules, channels and attachments created through MCP are marked as agent-created. `create_alert` adds `_l9_created_via_: last9-mcp` and `_l9_created_via_client_: <name>/<version>` rule annotations (overriding caller values); `create_alert_from_chart`, `create_notification_channel` and `attach_notification_channel` send `created_via` and `created_via_client` in their request bodies. The client string comes from the MCP session's `clientInfo`, with control characters and `{{`/`}}` removed, capped at 128 characters. `patch_alert` and `update_alert`, which replace annotations, keep the stored marker and drop caller attempts to set or forge it. `get_entity_alert_rules` prints a `Created Via` line and both `get_notification_channels` scopes add a `created_via` column; the `delete_alert`, `delete_notification_channel` and `detach_notification_channel` descriptions limit cleanup to marked resources unless the user names one (#349).
- Added `get_log_patterns` and `get_emerging_log_patterns` to the logs and investigate toolsets. The first clusters a service's sampled log lines into templates with count, first/last seen, severity mix and a sample; the second compares a current window against a baseline and returns only new, increased, decreased or disappeared templates with counts from both windows. Both link the sampled logs search (the current window for the second) in `reference_url`, like `get_service_logs`. Clustering runs in the MCP server over a bounded sample of the newest matching lines because the Last9 API has no log-pattern endpoint; templates and samples are cut at 500 characters so bodies that do not cluster keep the response small. The on-call and error-rate prompts check log novelty with them before reading raw lines (#333).
- In `--http` mode a request can narrow its tools with the `X-Last9-Toolsets` header or `toolsets` query parameter (a non-blank header wins). The effective set is the startup `--toolsets` intersected with the request's, so it never widens; unknown names return `400` naming them. Each distinct effective set gets its own server, with matching `tools/list` and instructions, built once and cached; a request restating the startup set reuses the startup server (#354).
- Added `get_workspace_overview`, the recommended first call for open-ended requests: it returns environments, whether logs, traces and metrics were seen in the last hour, service, alert-group and firing-alert counts, and the current time. The service count is the distinct services in `get_service_summary`'s `trace_endpoint_count` result, read with an instant query rather than the label index, which also listed services idle for the hour (41 against 39). Its seven backend calls run concurrently with a 10s timeout each; a failed or slow one leaves its field `null` with an `errors[]` entry instead of failing the tool. The server instructions now open with a "Start here" section recommending it when a request names no service or tool, and sending a request that names one straight to its tool; with `get_logs` served, the Reference manuals section also says to reference parse-stage fields as `attributes['name']`. It is served under every `--toolsets` value (#354).
- Added `get_current_time`: it returns the current time as UTC RFC3339 and epoch seconds, and resolves `range` expressions (`last 2h`, `yesterday`, `since yesterday 9am IST`, `since monday 10:00`, `<point> to <point>`) to absolute `start`/`end` in an IANA zone or abbreviation (IST → Asia/Kolkata), honouring DST; the ambiguous `CST` is rejected with both candidate zones, a weekday with a clock time still ahead today resolves to last week, `last` lookbacks are capped at 366d and starts before 1970 are rejected, and an unparseable point is quoted as typed with the accepted forms. It is served under every `--toolsets` value, and the binary now embeds the zone database so lookups work in the alpine image. Previously agents guessed today's date when building `*_time_iso` arguments (#354).
- `get_infrastructure_context` and `search_infrastructure_entities` (`metrics` and `investigate` toolsets) correlate hosts with Kubernetes. Search lists hosts, clusters, nodes or pods from the configured datasource's live Prometheus inventory (`node_uname_info`, `kube_node_info`, `kube_pod_info`) as typed ids, with a literal, case-insensitive name filter, pages of up to 100 and a `next_cursor`; filtering, deduplication and sorting run before the 500-entity cap, so a name query still finds entities past the first 500 in the inventory. Context resolves one host, node or pod through the Last9 API to its cluster, node, pods or host, with match status, evidence and a dashboard link, and sends no Prometheus credentials. `did_you_mean` accepts `type=k8s_node` (#224).
- `create_notification_channel`, `attach_notification_channel`, `detach_notification_channel` and `delete_notification_channel` (`alerts` toolset only) manage alert routing end to end. Create makes a Slack, PagerDuty, Opsgenie, email or generic-webhook channel after per-type client-side validation and returns only `id`, `name`, `type` and `send_resolved`; destinations, headers and certificates are never returned and upstream error bodies are not relayed. Attach routes a channel to an alert group per `severity` (`breach`, `threat`, or `both` = two attachments; if the second fails, the first stays attached and the error names the severity to retry) and carries the channel's delivery settings (webhook headers, CA certificate, Slack mentions) onto each attachment, refusing ids that are not channels. Detach removes one attachment by attachment id and refuses channel ids (the API would delete the channel), and needs a delete-scope token. Delete permanently removes a channel by channel id, refuses attachment rows and unknown ids without calling the API, maps the API's 422 (channel still attached) to a detach-first message, needs a delete-scope token, and re-lists channels afterwards so an unapplied delete is an error. `get_notification_channels` gains `scope` (`attached`, the default and unchanged, or `channels` listing `id`, `name`, `type`, `send_resolved` of the org's channels, without relaying upstream error bodies) (#190).
- `acknowledge_alert` (`alerts` toolset only) takes ownership of a firing alert as the token owner (`action` `ack`, the default) or releases it (`unack`), given `alert_group_id`, `rule_id` and `rule_type` from get_alerts. Acknowledging again as the owner is a no-op and returns the existing claim; an alert owned by someone else is reported with a pointer to `get_alerts`/`reassign_alert` rather than a raw 409. Optional `mute_until` only records a mute already applied with `mute_alert_group` or `patch_alert`; it does not mute (#190).
- `patch_alert` accepts `clear_recurring_mute_schedule=true` to remove a rule's recurring mute schedule entirely; it sends `"recurring_mute_schedule":null` in the PATCH body and is rejected alongside `recurring_mute_schedule`. `enabled=false` in a schedule only pauses it and keeps the windows (#190).
- `reassign_alert` (`alerts` toolset only) hands an already-owned alert to another user by `owner_email` (matched case-insensitively against active users) or `owner_user_id`, given the `alert_group_id`, `rule_id` and `rule_type` from `get_alerts`. 404, 403 and invalid-target-owner responses return explanatory tool errors; Last9 does not notify the new owner (#190).
- `mute_alert_group` (`alerts` toolset only) mutes every rule in an alert group until a Unix-seconds `until` deadline, or unmutes with `until=0` (negative mutes indefinitely). Use `patch_alert` to mute a single rule (#190).
- `create_alert_from_chart` accepts optional `group_id` (the chart's own alert group; the API rejects any other) and `description` (stored as the rule's `properties.description`), both passed through in the `/alert_intelligence` create request (#190).
- `validate_alert_expression` (read-only, `alerts` toolset) dry-runs the arithmetic expression over an alert group's KPIs (`indicators_used` is required; pass the inner metric expression, not an adaptive wrapper) and returns `{"valid":true}` or `{"valid":false,"reason":...}`; an invalid expression is returned as a result, not a tool error. `create_alert` and `update_alert` descriptions now point to it (#190).
- `patch_alert` accepts `recurring_mute_schedule` (`enabled`, IANA `timezone`, up to 20 weekly `windows` of `weekdays`, `start_time`, `end_time`), validated client-side with the same rules as the API and sent as `recurring_mute_schedule` in the rule PATCH body. A 403 from the PATCH now returns a tool error explaining that viewer-role users may only change `mute_until` and other changes need editor access (#190).
- Alert lifecycle tools: `create_alert` (always creates the rule disabled as a draft, ignoring `is_disabled=false`; promote with `patch_alert`), `update_alert` (full replace), `patch_alert` (pause, mute, metadata) and `delete_alert` manage alert rules on an existing alert group, `recommend_alert_config` proposes a rule design before creation, and `describe_alert_chart` / `create_alert_from_chart` create a static-threshold rule from a covered Discover chart. Rules using `adaptive_std_cmp(metric, sensitivity, window)` are saved as adaptive rules, with `condition`, `alert_condition` and `eval_window` optional (supplied values are kept, omitted ones get defaults). Upstream HTTP failures now surface as a typed error carrying the status code and sanitized body (#190).
- The server now sends `instructions` on `initialize` with a tool-selection policy: discover services, environments, attributes, metric/label names, alert rules and dashboards first; prefer the dedicated service, alert, change-event and deviation tools over raw `prometheus_*`, `get_logs` and `get_traces`; read the `last9://reference/...` manuals; create once and refine write targets, confirming destructive calls. The policy is rendered from `internal/prompts/server_instructions.md` against `--toolsets`, so it names only registered tools; previously clients got no instructions and chose tools from descriptions alone (#335).
- Added `did_you_mean` to the logs, traces, and metrics toolsets so their entity-name discovery guidance can be followed (#312).

### Fixed
- Every tool now accepts a number-like string for an integer argument, so `{"channel_id": "42"}` runs as `42` instead of failing with `type: 42 has type "string", want "integer"`. Before schema validation, a string matching `^-?\d+$` (and fitting int64) under a property whose schema allows integers but not strings, at any depth, is replaced by its number; other strings such as `"4x2"`, `"4.2"` or `""` are still rejected with the same error naming the argument, and unknown arguments are still refused (#373).

- `patch_alert` `properties` now changes only the fields sent: omitted or blank `description`, `runbook`, `annotations` and `query_mode` are carried over from the stored rule. The API overwrites the whole properties object, so sending only `description` cleared the runbook, annotations and query mode. `annotations` still replaces the whole map, and `{}` clears it (#356).
- stdio mode now flushes traces and metrics on exit: `log.Fatal` skipped the deferred telemetry shutdown, so spans from the last seconds and the final metric interval were dropped on every exit. A clean stdin close now exits 0 instead of 1, and SIGINT/SIGTERM exit 0 after the flush instead of killing the process, so supervisors that watch the exit code see a different status. Stdio also handles SIGINT/SIGTERM now: a signal cancels in-flight tool calls so the flush runs, and a second signal kills a stuck shutdown (#369).
- Name-only dashboard creation now starts with empty panels instead of inventing metric queries, matching the accepted dashboard schema (#322).
- `get_logs` now names `logjson_query` as the stage-array argument instead of the invalid `pipeline` key, and `get_traces` maps average duration requests to `$avg` rather than `$quantile` (#322).
- `create_dashboard` and `update_dashboard` now accept queryless CSV-backed panels through a `csv` panel branch in their shared schema, while remote-query panels still require `queries`; their descriptions say `log_raw` queries use `telemetry: "logs"` and a non-aggregate JSON pipeline (#322).
- Equal-window APM comparisons now go directly to the comparison tool when service scope is known, so a required profile lookup cannot consume the call budget before the comparison; the description also directs answers to use raw values when no percentage is returned (#322).
- Outbound API calls now inject W3C `traceparent` (and baggage) after `InitProviders` registers the global TextMapPropagator, so MCP spans can join backend traces when telemetry is enabled (#317).
- `prometheus_label_values` now documents that daily/sparse metrics need a widened `lookback_minutes` (for example `5760` for 4 days). The default 60-minute window omitted those series from discovery and led to false "not streaming" decisions; handler tests pin default, 4-day, and explicit ISO window wiring (#315).
- `create_dashboard` and `update_dashboard` no longer serve an integer `enum` for a panel's `version`; it is pinned to 1 with `minimum`/`maximum`, which providers that accept only string enums can load. A new `tool_metadata_test.go` check fails the build on any non-string enum, a description over its byte budget, a `last9://` URI with no registered resource, a schema nested deeper than 8, or a tool name that is not lowercase and verb-first (#354).
- Write tools no longer invite blind retries that can duplicate a create. When a write times out, its connection is reset or closed before a full response, its 2xx body cannot be read, or a gateway answers 502/504, the tool returns an error result `<tool> may or may not have been applied (<cause>). Do not retry until you have checked: <read call>.`, naming the read call that shows whether it landed (for example `get_alert_config` by rule name for `create_alert`), instead of "retry later" advice or, for an unread 2xx, a success. Failures to connect and definite HTTP statuses keep their existing errors. Each write makes exactly one attempt, and create-tool descriptions say to check by name before retrying; a toolset that serves a write tool must serve its verify tool, or the server fails to start (#351).
- `get_notification_channels` now shows `global` as false on alert-group attachment rows (`service_fqid` set). It relayed the API's flag, which reflects only a legacy service binding, so every per-group attachment read `global=true`; the description now says `services` lists only legacy service bindings (#190).
- `delete_notification_channel` given an attachment id (the `id` column of the default `attached` scope) now says it is an attachment, names its alert group and severity, and points to `detach_notification_channel`, instead of `no channel with that id`: the channel list it checked omits attachment rows (#190).
- `create_alert` with an `entity_id` that is not an alert group now says so and points to `get_alert_groups`, instead of `list alert rules failed with HTTP 400`, which named its internal duplicate-name lookup (#190).
- `get_entity_alert_rules` now shows each rule's `mute_until`, `recurring_mute_schedule` and `properties` (description, runbook, annotations) when set, and `get_alert_config` shows `mute_until` and `recurring_mute_schedule`, so changes made with `patch_alert` can be read back; previously a patched schedule or description was visible through no tool. The org-wide list behind `get_alert_config` returns only annotations in `properties`. Unset (null or empty) properties are omitted rather than printed as `annotations: <nil>`, and nested values such as `runbook` print as JSON (#190).
- `create_notification_channel` now says why the API rejected an `email` destination (a personal-provider address such as Gmail, a non-admin address on a personal-login org, or an address the API finds invalid) instead of only `the API rejected the channel (HTTP 400)`. The upstream body is still never relayed: only these fixed API messages are recognised (#190).
- `create_alert` and `update_alert` now name the missing field (`alert_rule.condition`, `alert_rule.alert_condition` or `alert_rule.eval_window`) when the API rejects a static rule without it, instead of relaying the raw `HTTP 400 … {"error":"condition required"}` (#190).
- `reassign_alert` with an `owner_email` that matches no active user now first checks the alert, so ids that match no alert report `no alert with this alert_group_id, rule_id and rule_type fired in the last hour` instead of `no active user with that email` (#190).
- The alert write-tool descriptions that promise "a link to Alerting" or "to notification channels" now say the link is in the result's `_meta.reference_url`; it was never in the result text, so agents reading only the text found none (#190).
- The `create_alert_from_chart` description and `threshold` schema now say the threshold must be 0 or more; the tool already rejected a negative value with `threshold must be a non-negative number`, but nothing told the agent beforehand (#190).
- The `get_notification_channels` description now says that `send_resolved` on attachment rows (the default `attached` scope) is the row's stored default, not what delivery uses: resolve notifications follow the channel's own value, shown by `scope=channels`. A channel created with `send_resolved:false` listed `true` on its attachments (#190).
- The `create_notification_channel` description no longer claims email destinations are limited to the org's own domain: the API accepts work addresses on any domain, rejects personal-provider addresses (for example Gmail), and accepts only the admin's address for orgs on a personal login (#190).
- `reassign_alert` now reports ids that match no alert as `no alert with this alert_group_id, rule_id and rule_type fired in the last hour` instead of `this alert has no owner yet`: on the API's 404, which covers both cases, it checks the alert monitor behind `get_alerts`, as `acknowledge_alert` does (#190).
- `create_alert` now refuses, without creating anything, a `rule_name` its alert group already uses (compared case-insensitively) and names the existing rule's id; the API accepted the duplicate, so a repeated or retried create silently added a second rule with the same name (#190).
- `validate_alert_expression` now accepts indicator names that its parser cannot read bare, such as KPI names with spaces (`Test Alert`), dots or dashes, by sending them as `[name]`. It used to report `Cannot transition token types from VARIABLE [Test] to VARIABLE [Alert]` for expressions that `create_alert` and `update_alert` save (#190).
- `describe_alert_chart` and `create_alert_from_chart` reject a `surface` other than `discover-service` or `discover-exceptions` before calling the API and point to `create_alert`, instead of relaying a misleading "unknown chart key". The describe description no longer promises a per-signal `name` (#190).
- `describe_alert_chart` and `create_alert_from_chart` label a 400/422 that is not a coverage miss (for example `group_id does not match chart group`) as `invalid_request` with fix-the-arguments guidance, instead of an upstream failure to retry (#190).
- `delete_alert` with a write-scope token now returns a tool error naming the delete-scope token it needs, like `detach_notification_channel` and `delete_notification_channel`, instead of generic credential advice (#190).
- `create_alert` and `update_alert` now explain the API's bare `Invalid expression_args` 400, which covers two mistakes: a KPI id that belongs to another alert group, or an `expression_args` key that is not the bound KPI's exact name. The message names both and how `validate_alert_expression` (`invalid indicator details for <name>`) tells them apart. `validate_alert_expression` accepts any name, so its description and schema no longer suggest placeholders such as `x, y, z` (#190).
- `acknowledge_alert` `ack` now refuses, without recording a claim, unless the alert monitor behind `get_alerts` lists that exact `alert_group_id`, `rule_id` and `rule_type` within the last hour; the claim API accepted any ids and wrote phantom claims (#190).
- `patch_alert`, `update_alert` and `delete_alert` now look the rule up first and refuse, without writing, when it does not exist or belongs to an alert group other than `entity_id`; the API acted on the rule id alone, so a mismatched pair changed or deleted another group's rule. An unknown id now reads "no alert rule … exists" instead of a generic 404 or a retryable 500 (#190).
- `create_notification_channel` now rejects a `generic_webhook` `destination` that is not an `https://` URL instead of silently creating a plain-HTTP channel; the description already documented an HTTPS-only destination (#190).
- `get_service_performance_details`, `get_service_operations_summary` and `get_service_dependency_graph` now build their `reference_url` deep link from the normalized literal `env` instead of the raw argument. For an anchored `env` such as `^production$`, the link previously carried the literal catalog filter `deployment_environment="^production$"`, which selects an environment distinct from the one the returned metrics were queried against (normally showing no data in the linked dashboard); an all-environments form such as `^.*$` also left a spurious catalog filter. The link now carries the matched environment name, or no filter at all for an all-environments `env` (#323).
- `get_service_performance_details`, `get_service_operations_summary`, `get_service_dependency_graph` and `get_database_queries` no longer return silent all-zero results for an anchored `env` such as `^production$` (the form `get_service_summary` documents). A full `^name$` wrapper is now matched as the literal name and `.*` means all environments; an `env` with alternation or a `.*`/`.+` wildcard is rejected with an error pointing to `get_service_environments`. Other names, including `prod[blue]`, still match literally as in #301. These tools (and the `get_database_queries` response) now echo the normalized environment actually matched in `env` (`production` for `^production$`, `.*` for an empty or all-environments value) instead of the raw argument, and add a top-level `env_requested` with the raw value only when it differed. The `env` parameter text on all four tools, and on `get_databases` and `get_service_summary` (whose regex matching is unchanged), now states the exact-match versus regex semantics and points to the right tool (#323).
- `get_service_profile` now includes observed domains and physical log indexes with their own environments in its investigation brief, enabling profile-derived index selection instead of silently querying the default index (#314).
- `get_traces` now distinguishes literal `TraceId` substring searches (`$contains`) from wildcard patterns (`$regex`) and shows the canonical `$avg` plus `groupby` shape for per-span duration queries (#312).
- `get_logs` now shows the field-first `$containsWords` form and stops percentile fallback after an empty result; dashboard create/update schemas validate query/legend types and logs-panel configuration, and descriptions document required log aggregates and time windows (#312).
- Log and trace tool descriptions now map message searches to `Body`, gRPC status code 14 to `attributes['rpc.grpc.status_code']`, and trace `service.name` to `ServiceName` (#312).
- `get_logs` now includes the required `type: "window_aggregate"` discriminator in its count and percentile examples, so copied stages pass validation (#312).
- Tool-call telemetry preserves the requesting agent's trace parent and records `mcp.turn.id` from trace context or explicit turn metadata; unrelated calls no longer share a synthetic session query span (#312).
- `get_logs` now permits standalone single-predicate filters and repeats counts after narrowing broad results; `get_service_logs` documents canonical gRPC status and directs multiple-status or range queries to `get_logs` (#312).
- `get_exceptions` now returns only the aggregate metric dimensions and occurrence count for each ranked row. It previously fabricated or emitted empty trace/sample fields, including a query-evaluation timestamp as `last_seen`, which made metric aggregates look like exact exception samples. The tool description and investigation workflow now require treating later service traces as context rather than verified matches (#310).
- Tool schemas now describe every argument, consistently mark required inputs, and use the canonical `$and` pipeline example across query helpers; public descriptions no longer expose transport routes (#309).
- `get_logs` and `get_traces` now explicitly show compact JSON stage-array examples and reject SQL-like query construction in their tool descriptions, so description-only clients can form valid pipelines without fetching a reference manual (#309).

### Changed
- When `get_logs` or `get_traces` is served, the server instructions' raw-query section now states that the time window goes in `start_time_iso`/`end_time_iso` (or `lookback_minutes`), never as `Timestamp` filters in the pipeline, instead of relying on the tool descriptions alone (#379).
- Tool empty-result, validation, timeout/rate-limit, and truncation responses now include actionable retry guidance so agents can widen or narrow time windows, verify filters, discover names with `did_you_mean`, and avoid unchanged retries instead of stopping at generic empty/error messages (#320).
- The `alerts` toolset now also serves `get_change_events`, the input `analyze_change_impact` takes as `change_event` (#352).
- The `on-call-runbook` prompt's latency and errors branches, `investigate-latency-spike` and `diagnose-error-rate` now look for a change with `get_change_events` right after `get_service_performance_details` and, when one precedes the symptom, judge it with `analyze_change_impact` before deeper triage: `no significant change` rules the change out, `insufficient data` does not (#352).
- Bumped 'modelcontextprotocol/go-sdk' 1.7.0 → 1.8.0 (#369).
- The 15 tools eval cases use most (`get_logs`, `get_service_summary`, `prometheus_range_query`, `get_service_logs`, `get_service_traces`, `get_exceptions`, `prometheus_label_values`, `get_apm_service_deviations`, `get_alerts`, `get_traces`, `get_alert_config`, `prometheus_instant_query`, `get_alert_rule_state`, `create_dashboard`, `list_datasources`) now carry *When to use*, *When not to use*, *Pairs well with*, *Output shape* and *Known traps* sections in their descriptions, naming the tool to use instead; previously only `get_service_summary` said when not to use it. `get_logs` leaves out *Pairs well with* and *Known traps*: its body already names those tools and traps, and in evals the restated sections pulled the model off its query pipeline. Under a narrowed `--toolsets` (or per-request toolsets), routes to tools that set does not serve are left out of the served description. A test fails if a section is missing or names a tool that is not served under any toolset serving the description. Description budgets for `get_traces` and `get_service_logs` rose by about 650 bytes each to fit, and `get_logs` by about 500 (#354).
- Server instructions now match what each tool does: `get_service_profile` is listed as the way to start a service investigation instead of as a service-discovery tool; `add_drop_rule` gets its own line saying it drops matching future logs instead of "overwrite or remove data", and the confirm-first and raw-query lines read correctly when one tool is served; the logs toolset, which serves no metric-name discovery tool, names `prometheus_instant_query` only for the `physical_index_service_count` log index inventory instead of as a general raw query tool; and the write section says `create_alert` rules are created disabled, pointing to `patch_alert` when it is served (#365).
- `get_alerts` now shows the episode claim (owner user id, name and email, claimed-by user id, `claimed_at` and `muted_until`) on each alert rule that has been acknowledged or owned, read from the `claim` object of the alert monitor response. Unclaimed rules are unchanged (#190).
- Bumped `github.com/last9/mcp-go-sdk` 0.1.4 → 0.1.5 (#313).
- Dashboard create and update schemas now restrict `query_type` to supported values and their descriptions explain the query types for logs, metrics, and traces (#312).
- `get_service_logs`, `get_service_traces`, and the PromQL range and instant query tools now serve explicit schemas for their supported fields, so unknown names and invalid field types are rejected during input validation (#311).

## [0.19.4] - 2026-10-06

### Added

- `get_service_logs` advertises and returns optional `last9/coverage` metadata only when its chunked query can verify the requested scope, distinguishing complete, partial, empty, and uncertain results (#307).

## [0.19.3] - 2026-09-30

### Added

- `get_alerts` now includes a `label_hash` field on each alert instance, exposing the hash of the instance's label set as used internally by the alerting engine. Useful for correlating an alert instance with its underlying metric series (#303).

## [0.19.2] - 2026-09-29

### Fixed

- `get_service_operations_summary`, `get_service_dependency_graph` and `get_database_queries` matched an explicitly supplied `env` as a live regex, so environment names containing regex metacharacters selected the wrong scope (`prod.v1` also matched `prodXv1`; `prod[blue]` matched `prodb` but not itself). Explicit env names are now regex-quoted before querying; an omitted env still matches all environments (#301).
- `get_service_performance_details` returned empty `response_times`, zero throughput, availability and error rates, and no top operations or errors when `env` was omitted, while `apdex_score` still had data: the unset-env default `.*` was applied with an exact `env=` matcher that matched no series. All sub-queries now use the regex `env=~` matcher, consistent with apdex and the sibling service tools; explicitly supplied env names are regex-quoted, so names containing regex metacharacters such as `prod.v1` still select only that environment (#300).

## [0.19.1] - 2026-09-23

### Changed

- `get_trace_attribute_values` now accepts `lookback_minutes` (default 15) and optional `start_time_iso` / `end_time_iso` for historical windows. Explicit ISO bounds take precedence over lookback. Previously the tool always queried a fixed last-15-minutes window with no way to inspect older spans (#297).

## [0.19.0] - 2026-09-23

### Fixed

- Token refresh no longer leaves a canceled tool call blocked for up to the 3-minute HTTP timeout while another call's refresh finishes, and canceling the call that started a shared refresh no longer aborts that refresh for concurrent waiters (which previously could hand them an expired token). `GetAccessToken` now wakes waiters on their own context cancel via `context.AfterFunc`, and runs the shared refresh on `context.WithoutCancel` so only the HTTP client timeout bounds it (#295).

### Added

- Every tool in `tools/list` now carries a `title` and MCP tool annotations, so clients can set permissions without guessing from the tool name. The 51 read tools are served with `readOnlyHint: true`, which lets a client run them without a per-call confirmation. The five write tools are served with `readOnlyHint: false` and an explicit `destructiveHint`: `true` for `add_drop_rule` (matching logs are dropped at ingestion), `update_dashboard`, `delete_dashboard` and `delete_dashboard_snapshot`, and `false` for `create_dashboard`, which only adds. Every tool also sets `openWorldHint: false`, because it acts only on the caller's own Last9 organization. Previously no tool had annotations, so clients fell back to the MCP defaults, which treat every tool as possibly destructive (#294).

## [0.18.0] - 2026-09-21

### Added

- Continuous profiling MCP tools backed by the same Profiling UI contract: `get_profile_services` (service index), `get_flamegraph` (nested tree), `get_top_functions` (self-sample ranking), and `get_profile_summary` (short NL triage). New `profiles` toolset; also included in `investigate`. Tenants without profiling enabled get a clear error asking them to contact the Last9 team (#214).
- `validate_dashboard`: read-only lint + execute + classify for a saved dashboard id or inline `dashboard_definition` over a ≤24h window. Returns `dashboard_validation/v1` (same contract as the supervisor skill). Dry run — never creates/updates dashboards. Day-1 empty results are `valid_no_data` without diagnose probes (#275).
- Five read-only Grafana tools, enabled under a new `grafana` toolset: `grafana_search_dashboards` (title substring search), `grafana_get_dashboard` (filtered summary by default: version, tags, templating variables, and each panel's type/datasource/gridPos/promQL targets; `full_json=true` returns the raw Grafana JSON), `grafana_list_folders`, `grafana_list_folder_dashboards`, and `grafana_list_datasources` (safe projection only — credential fields like `basicAuthUser`/`secureJsonFields` are never forwarded to the model). Tools hit the org Grafana API proxy authenticated with the same tenant MCP token; unknown panel types (plugin panels) are surfaced via `unsupportedPanelTypes` instead of being dropped. `grafana_list_folder_dashboards` resolves the folder's numeric id via `/api/folders/{uid}` then lists via `/api/search?folderIds=` (#289).

### Fixed

- Metrics tools that hit a Levitate "too many samples" 422 now return an actionable `METRICS_QUERY_TOO_MANY_SAMPLES` error that tells the agent to narrow filters or split the window (and never to ask the user to edit PromQL or retry the same query unchanged), instead of surfacing the raw upstream body (#221).
- `grafana_search_dashboards` and `grafana_list_folder_dashboards` now page through `/api/search` until a page comes back short or 5,000 dashboards are reached, instead of silently returning only the API's default first page (1,000 rows). Results are wrapped in `{"dashboards":[…], "truncated":bool}`; `truncated: true` signals that more dashboards exist than are returned, so a caller can no longer mistake a capped list for a complete inventory (#289).
- `create_dashboard` and `update_dashboard` now default every non-section panel to `version: 1` before the request reaches the API. The dashboards API now rejects new dashboards whose non-section panels omit `version` (last9/last9#11544), but the MCP server forwarded the model-generated dashboard JSON unmodified, so a versionless telemetry panel created through the tool bounced with a 400. `marshalDashboardRequest` now rewrites missing or zero `version` to `1` on every panel except `section` visualization panels, which the API exempts (#288).
- `get_alert_config`'s `search_term` now matches alert group team, tier, and metadata labels (label keys and values), not just rule name, alert group name/type, data source name, and tags. Commit 5d9757f added `Team` and `Labels` to `alertGroupEntityMetadata` and `Tier` to `alertGroupEntity` and rendered all three in every rule row, but did not extend `matchesAlertConfigSearchTerm` to search them, so a `search_term` that appeared only in team/tier/labels silently returned `Found 0 alert rules:` while the same rows displayed the matching data when no search term was used. The `search_term` prompt description is updated to list team, tier, and labels alongside the previously-searched fields so the prompt, the jsonschema tag, and the implementation agree (#285).
- `get_apm_service_deviations` no longer lets an improvement oust a cross-category regression from the `max_services` cap. `orderedDeviationSlices` interleaved each category's `Improvements` between that category's `Regressions` and the next category's `Regressions` (`Rel.Regr, Rel.Impr, Exp.Regr, …`), so a Reliability improvement consumed a cap slot — and was picked as the fleet follow-up target by `leadingDeviationIdentity` — before an Experience or SustainedLatency regression was even visited. `limitDeviationResult` then dropped the unvisited regression from `services` and the deviation leaderboards via `filterLeaderboardEntries`, and no downstream pass reinstated it: `shouldQueryOperations` and the corroborating follow-ups (`get_exceptions`, `get_service_logs`, `get_service_traces`) read the post-cap result and gate on `*.Regressions > 0`, so they never fired. At both `max_services=1` and the default `max_services=10`, a fleet's only regression could be silently deleted while improvements filled the cap and the follow-up was misrouted to an improving service. The slices are now ordered all `Regressions` before all `Improvements` (category-priority within each kind, magnitude within each category), so no improvement can outrank any regression across category boundaries — consistent with the regression-driven rest of the system. The committed category-over-magnitude priority among regressions (a low-magnitude Reliability regression still beats a high-magnitude Experience one) is preserved. The description's "magnitude-priority order" wording is corrected to "all regressions before all improvements, category-priority within each kind, magnitude within each category". Introduced by #264.
- `get_trace_attribute_deviations` no longer forwards a folded `{"$and":[…]}` as a single `filters` element when `$notnull`/`$exists` was rewritten alongside a sibling operator. The filters array is AND-implicit, so that shape is split into sibling bare field-operator conditions (or rejected for `$or`/`$not`). Scope filters that use `TraceId`, `SpanId`, `ParentSpanId`, `TraceState`, or `Timestamp` are also rejected locally — the deviations endpoint returns HTTP 422 for those fields even though they are valid on `get_traces` (#283).
- `get_databases` now lists databases discovered from infrastructure metrics, not only from OpenTelemetry client spans. Previously a database with no trace-backed client spans — an OpenSearch or RDS instance reporting through CloudWatch, for example — was reported as "No databases found" even though the Databases dashboard showed it. Rows now carry `sources`, `metrics_only`, `capabilities`, `activity`, and `resolved_labels`, and a row omits `throughput_rpm`, `p95_latency_ms`, `error_rate_pct` and `service_count` when it has no trace-backed values instead of reporting zeros (#277).
- `get_alert_groups`: the `label_key` + `label_value` filter is now truly case-insensitive on both coordinates. `matchesAlertGroupEntityFilters` resolved the entity's labels to a single value first (exact-byte key, else the lexicographically-lowest case-variant key) before comparing it to the query, so when an entity carried duplicate keys differing only by case with different values (e.g. `domain=checkout` and `Domain=other`), the same semantic query matched or failed depending on the casing the caller typed — `label_key="domain"` matched while `label_key="DOMAIN"` dropped the same entity. The matcher now iterates the labels and accepts the entity when any `(key, value)` pair matches case-insensitively on both coordinates via `strings.EqualFold`, so the result no longer depends on query-key casing. `get_alert_config` is unaffected — its args struct never sets `label_key`/`label_value`, so the label branch was never entered there (#286).

### Changed

- Bumped `go.opentelemetry.io/otel/exporters/otlp/otlptrace` and `…/otlptracehttp` 1.43.0 → 1.45.0 (#290, #292).

## [0.17.0] - 2026-09-11

### Added

- `get_alert_groups`: lists configured Compass alert groups with team, tier, metadata labels, and rule counts — including groups with zero rules and groups that are not currently firing. `get_alert_config` rule rows now also print team, tier, and labels when those fields are set (#222).
- `last9://reference/investigation`: an MCP resource documenting the profile-first investigation flow (#209).
- `get_service_profile` returns a per-service telemetry profile — signal presence, language/runtime, deployment envs, log `signal_shape`, and a recommended ingest fix — as a short brief followed by raw JSON. Call it before a service-scoped investigation to skip trace tools when traces are absent and to parse severity from the log body when `severity_set` is `none` or `partial` (#208).

### Fixed

- HTTP mode (`--http`): tool calls now record which client made them. Previously every client shared one `unknown_client` identity, so unrelated sessions were mixed together. Bumps `last9/mcp-go-sdk` to v0.1.4 and `modelcontextprotocol/go-sdk` to v1.7.0 (#274).
- `get_service_operations_summary` no longer drops the service's own inbound endpoints. The handler builds a `ServiceOperationSummary` per `span_name` in four structurally-identical loops — server, DB-client, HTTP-client and messaging-producer spans — but the `SPAN_KIND_SERVER` loop never appended its result, so every inbound HTTP endpoint the service exposes was silently missing from the `operations` array while its outbound calls were present. Any latency or error triage that started from this tool saw only the service's dependencies, never its own routes (#269).
- `l9_sanity`'s `service_log_volume` baseline no longer under-counts for non-whole-minute query windows. The PromQL `sum_over_time(...[Nm])` window used floor division on the request duration, dropping up to ~59 s of the query window for any non-whole-minute `start_time_iso`/`end_time_iso` range. On the nonzero path this inflated the ratio and produced false "filter is likely too broad" notes; on the zero path a service emitting only in the dropped prefix was misclassified as a "genuine zero ... nothing to inspect", directing the model away from the `sample_bodies` inspection the guardrail exists to trigger. The baseline now ceiling-divides so its window is never shorter than the query window — exact for whole minutes, over-covering by at most ~59 s otherwise (the conservative direction for both paths). Whole-minute windows are unchanged (#268).
- `get_alert_config` no longer applies unconfigured-only behavior to a mixed result set. When `only_without_notification_channel` was OR-combined with `notification_channel_types`/`names`/`severities`, the response also contained configured rules matched through the channel-binding branch of the OR, but the handler still skipped KPI resolution for the whole set, used the unconfigured-only count header, and prepended the global-channel advisory. KPI resolution now runs, the header reflects the real set, and the advisory is prepended only for a pure unconfigured-only query with no active channel filters (#266).
- `get_apm_service_deviations` now keeps the highest-magnitude regression through the `max_services` cap instead of an alphabetically-first slice of the fleet. The cap previously walked the alphabetically-sorted `services` list, so a fleet with more than `max_services` monitored identities (default 10) whose top regression was alphabetically late dropped it from `services`, the leaderboards, and `throughput_shifts`; `leadingDeviationIdentity` then picked the magnitude leader of the capped set, misrouting the `get_apm_service_deviations` fleet follow-up to a lower-magnitude service. The cap now admits identities in the magnitude-priority order the leaderboards and follow-up already use (regressions and improvements by `|relative_delta|`, then `throughput_shifts`, then telemetry changes), and fills any remaining capacity with stable services alphabetically — preserving the prior behavior for fleets with no deviations (#264).
- Trace and log tools reject malformed OpenTelemetry identifiers locally instead of forwarding them. A 16-character span ID passed as `trace_id` now names the mistake instead of returning an upstream `HTTP 400 Invalid traceId`, and uppercase IDs are folded to lowercase. Malformed IDs in a filter error instead of silently matching nothing — in `get_traces` on all four exact-match operators, on the log pipeline's `TraceId`/`SpanId`/`ParentSpanId` fields, and in the previously unvalidated pipelines of `get_trace_attributes_for_pipeline`, `get_trace_attribute_values`, and `get_trace_attribute_deviations`. Substring and regex operators still match partial IDs (#211).
- `get_service_performance_details`, `get_service_operations_summary` and `get_service_dependency_graph` now escape `service_name` and `env` before interpolating them into PromQL label matchers. The three handlers formatted fully caller-controlled values into single-quote-delimited matchers (`service_name='%s'`, `env=~'%s'`, `server='%s'`, `client='%s'`) with no escaping, so a `'` in a value closed the string literal early and injected arbitrary PromQL into the surrounding selector. Values containing quotes now match literally instead of altering the query (#265).
- The `/health` endpoint in HTTP mode (`--http`) now reports the running build's version instead of a hardcoded `1.0.0`, matching `--version`, the startup banner, and the MCP server-init log (#262).
- `get_apm_service_deviations` no longer reports genuine reliability regressions as `stable`. The `error_percentage_distribution` grid divides an error expression whose top-level operator is `or` — it unions error counts with a `(requestExpression * 0)` zero-fill branch — but that expression was interpolated bare as the left operand of `/`. PromQL binds `/` tighter than `or`, so the division applied to the zero-fill branch alone and the resulting percentages collapsed toward zero, flattening real error-rate deviations into the `stable` bucket. The union is now parenthesized (#259).
- The logs pipeline sanitizer now normalizes a map-form `$not` (`{"$not": {…}}`) to the documented single-element array form (`{"$not": [condition]}`). A map-form `$not` on `Body` previously survived sanitization unchanged and was skipped by the chunking-throttle and count-sanity heuristics, which only descend into an array-form `$not`: a non-aggregate `Body` search over a >1d lookback ran with ~3× too many parallel chunks, and a zero-count `Body` aggregate dropped its `l9_sanity` diagnostic. Array-form `$not` was already correct and is unchanged (#258).
- The server no longer dies with an unrecoverable `fatal error: sync: Unlock of unlocked RWMutex` when a tool call overlaps an in-flight OAuth token refresh. `TokenManager`'s refresh condition variable was built over the manager's `sync.RWMutex`, while `GetAccessToken` held only a read lock when it reached `Wait()`; `sync.Cond.Wait` unlocks and relocks its `Locker`, which for an `RWMutex` resolves to the *write* lock, so the wait released a lock it never held and took down the whole process — not just the failing call. The condition now has its own dedicated mutex (#257).
- `get_service_traces` now sets `isError` when the upstream `query_range` API returns HTTP 200 with a body missing `data` or `data.result`. On the `service_name` branch the handler overwrote the transform's diagnostic ("Invalid API response: missing data field") with a `Retrieved 0 traces` summary and returned `isError=false` alongside a `success:false` JSON body — the only path in the `traces` package that disagreed with its own payload, so a malformed response was indistinguishable from a genuinely empty result. The diagnostic is preserved and the failure is escalated, with the structured body and dashboard deep link unchanged (#256).
- `get_database_slow_queries` now applies `min_duration_ms` to log-sourced results. The tool merges slow queries from the traces API, which honored the filter server-side via `$gte` on `Duration`, and from the logs API, which never referenced it and kept every parsed entry whose duration was merely above zero — so a caller asking for queries slower than a threshold got back faster ones from the log half. The duration lives inside the JSON log body under instrumentation-specific keys and cannot be pushed into the server-side pipeline, so the threshold is enforced client-side; note the logs API returns at most `limit` entries before that filter, so qualifying slow logs beyond that page may be missed on this best-effort path (#255).
- `get_drop_rules` and `add_drop_rule` now route non-2xx `/otel_settings/drop` responses through the shared upstream sanitizer (URL/credential redaction, 512-byte truncation with `… (truncated)`, body drained and omitted for 5xx and other non-400/422) instead of echoing the raw body via an unbounded `io.ReadAll` into the tool error surfaced to the model. This matches the `get_logs` / `get_service_logs` contract (#254).

### Changed

- `create_dashboard` and `update_dashboard` descriptions now steer create-once, refine-with-update. A successful `create_dashboard` appends a second text part pointing at `update_dashboard` with the new id; `Content[0]` is still the raw API JSON (#215).
- `get_service_performance_details` now returns about 200 points per series instead of one per minute, sizing each range selector from the resulting step. Windows under ~3h20m are unchanged; above that the `rate()`-based series (availability, throughput, error rate, error percent) aggregate the whole window, while apdex and response times stay last-value and get sparser. Response-time values shift slightly — that query's lookback is no longer fixed at 5m (#232).
- `get_apm_service_deviations` no longer pre-filters candidates with a traffic-based `topk` mask: the deviation math now runs over every identity in scope (service and operation scope alike), and the magnitude-aware in-process selection (`limitDeviationResult` for services, the correlation trim for operations) performs the only cut. Previously the mask fetched exactly `max_services` identities ranked purely by traffic volume, so a low-traffic service with a high-magnitude regression could be excluded before any deviation math ran and could never be recovered downstream; deviation classification depends on per-bucket distribution overlap and evidence coverage that no PromQL ranking proxy can mirror, so any pre-filter breaks completeness. Results are now complete with respect to the data; the trade-off is that query cost scales with the number of identities in the selected scope, so narrow with `env` or `service_name` on very large fleets. When the datasource rejects or times out on queries in this class (429/408/413/422/503/504 or a client-side timeout), the failure now surfaces as a `query_rejected` partial error (previously the generic `query_failed`) plus a warning advising to narrow the scope with `env` or `service_name` and retry. Operation-scope calls also warn when a service reports more than 500 distinct span names, since unbounded span-name cardinality (URLs with embedded IDs) is the main cost amplifier of an unmasked scoped call (#264).
- `get_apm_service_deviations` docs now state that explicit `start_time_iso`/`end_time_iso` and baseline timestamps must align to whole minutes (seconds `:00`; unaligned timestamps were already rejected but the requirement was undocumented), and that `env` is an exact literal match rather than a regex, unlike the `env` parameter on sibling tools such as `get_service_summary` (#264).
- The service workflows and the service-scoped tool descriptions now call `get_service_profile` first: skip trace tools when the profile reports `telemetry.traces` as `absent`, and parse the level from the log body when `severity_set` is `none` or `partial` (#209).
- Bumped `google.golang.org/grpc` 1.82.1 → 1.83.2 (#231, #272).

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
