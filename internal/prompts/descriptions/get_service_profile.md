Call this tool before any service-scoped investigation (logs, traces, exceptions, latency, errors). Returns a telemetry profile for one service: signal presence, severity routing, and ingest hints.

**Tri-state semantics:** telemetry fields use `present`, `absent`, or `unknown`. `unknown` means not yet derived — not the same as `absent`. Do not treat unknown as missing data.

**severity_set routing:** when `severity_set` is `partial` or `none`, parse severity from the body field (`level_field` in the response). Do not use `severity_filters` on get_service_logs — route like `none`.

**Domain and log index routing:** `domains` lists observed domains; `domain_envs` maps each domain to its observed environments. `log_indexes` lists physical log indexes; `log_index_envs` maps each index to its observed environments. The brief preserves their reported order and environment associations. Missing or empty fields provide no routing evidence; never infer their environments from `deployment.envs`.

For a requested environment, use matching `log_index_envs` entries to select from `log_indexes`. Query each matching index separately when several match. Pass a non-default index as `index: "physical_index:<name>"` to log tools; for `default`, omit `index`. Never invent a default index from missing routing fields.

**Trust but verify:** the profile is derived from recent telemetry (~15 minute TTL). If a later tool call contradicts the profile, trust the fresh evidence and note the mismatch.

**Output:** a short investigation brief followed by a blank line and the full profile as raw JSON. When logs and traces are both `absent`, the name may simply be wrong — confirm it with `did_you_mean` before concluding the service is unmonitored.

Full investigation orchestration guide: `last9://reference/investigation`

Parameters:
- service_name: (Required) Service to derive a telemetry profile for
- datasource: (Optional) Datasource name. Omit for default.
