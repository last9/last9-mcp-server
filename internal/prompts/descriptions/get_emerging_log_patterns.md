Answer "what is new or different in this service's logs right now?" Clusters log lines from a current window and a baseline window into shared message templates (variables become `<*>`) and returns only the templates that are new, increased, decreased or disappeared, with counts from both windows. Use it first in triage instead of paging raw lines with `get_service_logs` or `get_logs`; read raw lines only to drill into a template found here. To see everything a service logs in one window, use `get_log_patterns`.

Parameters:
- service_name: (Required) Service whose logs to compare.
- env: (Optional) Environment, e.g. `production`. Omit when unknown.
- start_time_iso / end_time_iso: (Optional) Current window bounds, RFC3339. Default: the last `lookback_minutes`.
- lookback_minutes: (Optional) Current window length in minutes when `start_time_iso` is omitted. Default 60, minimum 1.
- baseline_start_time_iso / baseline_end_time_iso: (Optional) Baseline window, RFC3339, set both or neither. It must end at or before the current window starts. Default: the window of equal length immediately before the current one. For "same time yesterday", pass that window explicitly.
- severity_filters: (Optional) Severities to keep, OR logic, e.g. `["error","fatal"]`. Omit to compare every severity.
- body_filters: (Optional) Case-insensitive message substrings to keep, OR logic.
- index: (Optional) `physical_index:<name>` or `rehydration_index:<block_name>` from `get_service_profile`. Omit for the default index.
- sample_size: (Optional) Maximum lines sampled per window. Default 1000, maximum 5000.
- min_count: (Optional) Minimum lines a pattern needs in the window it is larger in to be reported. Default 3.
- change_factor: (Optional) Frequency ratio that counts as significant, greater than 1. Default 2 (doubled or halved).
- top_n: (Optional) Changed patterns returned. Default 20, maximum 100. The rest are totalled in `other` (`patterns`, `current_count`, `baseline_count`).

Returns:
- `current` and `baseline`: each window's range, `sampled_lines`, `sample_limit_reached` (true when the sample may miss matching lines), `total_matching_lines` when the API reports it, and `logs_truncated` when the search stopped early.
- `comparison_basis`: `rate_per_minute` when both samples hold every matching line, `share_of_sample` when either has `sample_limit_reached`.
- `patterns`: each with `template`, `change` (`new`, `increased`, `decreased`, `disappeared`), `ratio` (current over baseline frequency, when both are non-zero), `current` and `baseline` stats (`count`, `first_seen`, `last_seen`, `severity_counts`) and one raw `sample` line. Ordered new, increased, decreased, disappeared.
- `unchanged_patterns`: templates seen without a significant change. `note`: set when the baseline had no lines, so everything current reads as new.

Rules:
- An empty `patterns` list is a valid answer: nothing in the logs changed significantly. Report that there is no log novelty. Do not invent patterns, counts or causes that the response does not contain, and do not lower `min_count` or `change_factor` just to produce a finding.
- Counts are within each window's sample; quote them with `comparison_basis`. Failed search chunks return a tool error. A truncated or capped search still returns a comparison, with `sample_limit_reached` true on that window and `comparison_basis` `share_of_sample`.
- When either window has `sample_limit_reached`, `new` and `disappeared` mean absent from the other window's sample, not from the window; say so when you report them.
