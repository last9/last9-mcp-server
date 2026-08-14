Fetch a nested continuous-profiling flamegraph for one service.

Calls POST /profiles/api/v1/query_range/json, aggregates stacks by StackHash,
and derives a tree (name/value/self/children) client-side — same contract as
the Profiling UI.

Critical rules:
- service is required. Discover candidates with get_profile_services first.
- Default profile_type is cpu. Always pin a type; never omit it when comparing
  windows or you will mix units.
- value = inclusive samples; self = exclusive samples on that frame.
- truncated=true means the API row limit was hit; tighten filters or the window.
- Prefer lookback_minutes OR explicit start_time_iso/end_time_iso; default 60m.

Parameters:
- service: (Required) ServiceName
- env / cluster / namespace / runtime: optional filters
- profile_type: cpu (default), alloc, or wall
- limit: max aggregated stack rows (default 1000)
- region / lookback_minutes / start_time_iso / end_time_iso
