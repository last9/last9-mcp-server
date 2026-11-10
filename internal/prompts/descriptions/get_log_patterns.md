Cluster one service's log lines in a time window into message templates (variables such as ids, numbers and timestamps become `<*>`) and return the most frequent ones. Use it during triage to see what a service is logging instead of paging raw lines with `get_service_logs` or `get_logs`; read raw lines only to drill into a template you found here. To ask what is new or different compared with an earlier window, use `get_emerging_log_patterns`.

Returns:
- `window`: the sampled range, `sampled_lines`, `sample_limit_reached` (true when the sample may miss matching lines), `total_matching_lines` when the API reports it, and `logs_truncated` when the search stopped early (its total is then only a floor).
- `patterns`: each with `template`, `count`, `first_seen`, `last_seen`, `severity_counts` and one raw `sample` line; templates and samples are cut at 500 characters, ending in `…`.
- `total_patterns` and `other` (`patterns`, `count`) for templates below `top_n`.

Rules:
- Counts, `first_seen` and `last_seen` are within the sample of at most `sample_size` lines. When `sample_limit_reached` is true the sample holds only the window's newest lines: report shares of the sample, not absolute volumes, or narrow the window or filters.
- An empty `patterns` list with `sample_limit_reached` false is a valid answer: the service logged nothing matching in that window. Say so. An empty `patterns` list with `sample_limit_reached` true is not proof of an empty window: the search stopped before returning lines, so say the sample is empty, not the window, and narrow the window or retry. Failed search chunks return a tool error. A truncated or capped search still returns patterns, with `sample_limit_reached` true: apply the share-of-sample rule above. Do not invent patterns, counts or causes that the response does not contain.
