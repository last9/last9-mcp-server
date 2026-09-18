Natural-language summary of a service's continuous profile hot spots.

Returns a short sentence like "Top 3 CPU consumers are X, Y, Z accounting for
N% of self samples" plus the underlying top_functions rows. Uses the same
capped flamegraph stack fetch as get_flamegraph / get_top_functions.

Critical rules:
- service is required. Prefer get_profile_services when unsure a service has data.
- Default profile_type is cpu. Do not mix profile types across comparisons.
- Use this for quick triage; use get_flamegraph for stack structure and
  get_top_functions for a longer ranked list.
- total_samples and percentages cover only the returned stack rows. When
  truncated=true the upstream row cap was hit — the summary may omit hotter
  functions outside the returned subset; do not treat it as a full-profile
  guarantee.
- Prefer lookback_minutes OR explicit start_time_iso/end_time_iso; default 60m.
- If profiling is not enabled for the account, the tool returns an error asking
  the user to contact the Last9 team.

Parameters:
- service: (Required) ServiceName
- env / cluster / namespace / runtime: optional filters
- profile_type: cpu (default), alloc, or wall
- top_n: how many consumers to mention (default 3, max 10)
- region / lookback_minutes / start_time_iso / end_time_iso
