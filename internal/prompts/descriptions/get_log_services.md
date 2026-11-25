Lists services that send logs, with their environments, physical indexes and when each last reported, from the log-index service count metric. Use it to offer the user a list of services for a log request: unlike APM server-span inventories, it includes log-only services.

Parameters:
- `query`: a service name to rank by. Each row gets `similarity` (percent, word order ignored, so `beta-alpha-gamma` scores 100 for `alpha-beta-gamma`); rows sort by similarity, then name. Omitted: rows sort by name.
- `env`, `index`: exact environment and physical index name filters (index `default` is the default index).
- `lookback_minutes` (default 60, minimum 1) or `start_time_iso`/`end_time_iso` (RFC3339; take precedence). With no window and nothing found, the lookup retries once over the last 4 days and says so in `note`.
- `limit`: rows to return, 1-100 (default 10).
- `datasource`: omitted uses the default; a near miss uses its closest match, named in an extra content block.

Output: `{rows: [{name, envs, indexes, last_seen, similarity?}], total, returned, truncated, note?}`. `total` counts every matching service even when `limit` cuts `rows`; when `truncated` is true, tell the user "`returned` of `total`". `last_seen` is RFC3339 or null when unknown.

An empty list, or a `note` saying discovery is incomplete, is not proof a service sends no logs: confirm with `get_logs` before saying it does not exist.