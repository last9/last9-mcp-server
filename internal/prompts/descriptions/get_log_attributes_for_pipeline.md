Returns log fields present AFTER the given pipeline, each with the exact `filter_field` for `get_logs` conditions.

Scoped to your pipeline (not the global `get_log_attributes` catalog). Workflow: filter stage → this tool → use returned `filter_field` in `get_logs`. Do not assume field key names — confirm here (`status_code` vs `http.status_code` vary by scope).

Each entry: `name`, `filter_field` (use directly, no transforms), `hint`, optional `source`/`sample_coverage`.
- `source`=`"body"`: field only inside log Body; hint names parse stage (json/logfmt/regexp). Add that parse before filter/groupby or values are empty. Prefer indexed severity/level when present.
- `sample_coverage`: body-field row coverage; prefer full (e.g. `"5/5"`), avoid sparse keys.

Default window: last 15 minutes.

Time: prefer `lookback_minutes`; `start_time_iso`/`end_time_iso` (RFC3339) for absolute. Legacy `YYYY-MM-DD HH:MM:SS` accepted.

Index: only when user names one — `physical_index:<name>` or `rehydration_index:<block_name>`. Omit otherwise. Inventory via `physical_index_service_count` label `name`; `name="default"` → omit index. If backend rejects index filter, retry without index.
