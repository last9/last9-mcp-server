Returns a bounded, evidence-qualified catalog for the requested log and trace sources. Catalog schema version: `1`.

Use absolute RFC3339 bounds. `datasource`, `sources`, `protocol`, `start_time_iso`, `end_time_iso`, and `include` are required. `include` accepts `services`, `environments`, and `fields`. Optional `log_index` selects `physical_index:<name>` or `rehydration_index:<block_name>` for log discovery; omit it for the default log index. Optional `limit` controls services or environments per source (default 100, maximum 100).

Only `l9_result.partial=false` observations with configured source contracts are complete enough for numeric conclusions. Missing contracts and backend limits are reported as partial; do not infer missing values from them.
