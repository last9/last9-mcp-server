Returns log fields present AFTER the given pipeline, each with the exact `filter_field` for `get_logs` conditions.

Scoped to your pipeline (not the global `get_log_attributes` catalog). Workflow: filter stage → this tool → use returned `filter_field` in `get_logs`. Do not assume field key names — confirm here (`status_code` vs `http.status_code` vary by scope).

Each entry: `name`, `filter_field` (use directly, no transforms), `hint`, optional `source`/`sample_coverage`/`sample_body`.
- `source`=`"body"`: field only inside log Body; hint names parse stage (json/logfmt/regexp). Add that parse before filter/groupby or values are empty — unless `sample_body` is present (see below). Prefer indexed severity/level when present.
- `sample_coverage`: body-field row coverage; prefer full (e.g. `"5/5"`), avoid sparse keys.
- `sample_body`: present on the plaintext-inline severity (`level`) entry, and on the plaintext fallback entry when Body has no recognized JSON/logfmt/severity structure at all — a redacted sample line, the latter alongside a `$regex`-on-Body hint. Anchor your pattern to that exact shape; do NOT use `parser:"json"` (or any parser) against the fallback entry. The sample is REDACTED and NORMALIZED: `[redacted-url]`/`[redacted-credential]` are placeholders, not literal log text, and repeated whitespace is collapsed to a single space — anchor on the surrounding structure, not on those placeholders or on exact column spacing.

Default window: last 15 minutes.

Time: prefer `lookback_minutes`; `start_time_iso`/`end_time_iso` (RFC3339) for absolute. Legacy `YYYY-MM-DD HH:MM:SS` accepted.

Index: only when user names one — `physical_index:<name>` or `rehydration_index:<block_name>`. Omit otherwise. Inventory via `physical_index_service_count` label `name`; `name="default"` → omit index. If backend rejects index filter, retry without index.
