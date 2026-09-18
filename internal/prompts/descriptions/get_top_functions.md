Rank hottest functions for a service by self (exclusive) sample share.

Fetches the same flamegraph stack rows as get_flamegraph (default 1000
aggregated stacks, sample-descending), then folds frames into a sorted
top-functions table with self/total samples and percentages.

Critical rules:
- service is required. Use get_profile_services when the service name is unknown.
- Optimize by self_percent / self_samples first — that is exclusive cost.
- Default profile_type is cpu; pin the type explicitly for alloc/wall questions.
- limit caps the ranked list after folding (default 50), not the upstream fetch.
- total_samples and percentages cover only the returned stack rows. When
  truncated=true the upstream row cap was hit — rankings may omit hotter
  functions outside the returned subset; tighten filters/window or raise the
  flamegraph row limit via get_flamegraph before trusting the ranking.
- Prefer lookback_minutes OR explicit start_time_iso/end_time_iso; default 60m.
- If profiling is not enabled for the account, the tool returns an error asking
  the user to contact the Last9 team.

Parameters:
- service: (Required) ServiceName
- env / cluster / namespace / runtime: optional filters
- profile_type: cpu (default), alloc, or wall
- limit: max ranked functions (default 50)
- region / lookback_minutes / start_time_iso / end_time_iso
