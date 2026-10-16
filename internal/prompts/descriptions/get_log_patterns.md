Cluster one service's log lines in a time window into message templates (variables such as ids, numbers and timestamps become `<*>`) and return the most frequent ones. Use it during triage to see what a service is logging instead of paging raw lines with `get_service_logs` or `get_logs`; read raw lines only to drill into a template you found here. To ask what is new or different compared with an earlier window, use `get_emerging_log_patterns`.

Parameters:
- service_name: (Required) Service whose logs to cluster.
- env: (Optional) Environment, e.g. `production`. Omit when unknown.
- start_time_iso / end_time_iso: (Optional) Window bounds, RFC3339. Default: the last `lookback_minutes`.
- lookback_minutes: (Optional) Window length in minutes when `start_time_iso` is omitted. Default 60, minimum 1.
- severity_filters: (Optional) Severities to keep, OR logic, e.g. `["error","fatal"]`. Omit to cluster every severity.
- body_filters: (Optional) Case-insensitive message substrings to keep, OR logic.
- index: (Optional) `physical_index:<name>` or `rehydration_index:<block_name>` from `get_service_profile`. Omit for the default index.
- sample_size: (Optional) Maximum lines sampled from the window. Default 1000, maximum 5000.
- top_n: (Optional) Patterns returned, by count. Default 20, maximum 100. The rest are totalled in `other`.

Returns:
- `window`: the sampled range, `sampled_lines`, `sample_limit_reached` (true when the sample may miss matching lines), `total_matching_lines` when the API reports it, and `logs_truncated` when the search stopped early (its total is then only a floor).
- `patterns`: each with `template`, `count`, `first_seen`, `last_seen`, `severity_counts` and one raw `sample` line.
- `total_patterns` and `other` (`patterns`, `count`) for templates below `top_n`.

Rules:
- Counts, `first_seen` and `last_seen` are within the sample. When `sample_limit_reached` is true the sample holds only part of the window: report shares of the sample, not absolute volumes, or narrow the window or filters.
- An empty `patterns` list with `sample_limit_reached` false is a valid answer: the service logged nothing matching in that window. Say so. An empty `patterns` list with `sample_limit_reached` true is not proof of an empty window: the search stopped before returning lines, so say the sample is empty, not the window, and narrow the window or retry. Failed search chunks return a tool error. A truncated or capped search still returns patterns, with `sample_limit_reached` true: apply the share-of-sample rule above. Do not invent patterns, counts or causes that the response does not contain.
