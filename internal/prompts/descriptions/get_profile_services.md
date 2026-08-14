List services that have continuous profiling samples in a time window.

Returns each service's sample weight, relative share, dominant runtime
(telemetry.sdk.language), and last profile timestamp when available.

Use this before get_flamegraph / get_top_functions / get_profile_summary to
discover which services actually have profile data.

Critical rules:
- Default profile_type is cpu. Pinning a type avoids mixing CPU nanoseconds
  with allocation bytes into one meaningless ranking.
- Prefer lookback_minutes OR an explicit start_time_iso/end_time_iso pair;
  default lookback is 60 minutes.
- Empty services means no profile samples in range — do not invent services.
- If profiling is not enabled for the account, the tool returns an error asking
  the user to contact the Last9 team.

Parameters:
- env / cluster / namespace / runtime: optional resource filters
- profile_type: cpu (default), alloc, or wall
- region: optional override of the configured datasource region
- lookback_minutes: default 60
- start_time_iso / end_time_iso: RFC3339; override lookback when set
