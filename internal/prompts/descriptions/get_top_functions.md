Rank hottest functions for a service by self (exclusive) sample share.

Fetches the same flamegraph stack rows as get_flamegraph, then folds frames
into a sorted top-functions table with self/total samples and percentages.

Critical rules:
- service is required. Use get_profile_services when the service name is unknown.
- Optimize by self_percent / self_samples first — that is exclusive cost.
- Default profile_type is cpu; pin the type explicitly for alloc/wall questions.
- limit caps the ranked list after folding (default 50), not the upstream fetch.
- Prefer lookback_minutes OR explicit start_time_iso/end_time_iso; default 60m.

Parameters:
- service: (Required) ServiceName
- env / cluster / namespace / runtime: optional filters
- profile_type: cpu (default), alloc, or wall
- limit: max ranked functions (default 50)
- region / lookback_minutes / start_time_iso / end_time_iso
