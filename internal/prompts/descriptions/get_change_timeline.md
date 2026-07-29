Build one evidence-preserving chronology from recorded Change Events and observed alert episodes. Use this first for incident ordering, nearby changes, mitigation timing, and affected scope. Temporal proximity and matching labels are correlation, never causality.

Use lookback_minutes (default/max 60) or an explicit RFC3339 start/end pair spanning at most one hour. Optional filters include service, environment, alert group, rule, and event name. kinds accepts change_event and/or alert_episode; max_events defaults to 200 (max 500). Use get_change_events to discover exact event names.

Treat rows as recorded changes or first/last observed alert firings, not exhaustive changes or inferred incident/resolution times. Respect complete, partial, and failed coverage; cite timestamps and sources; use recommended follow-ups selectively.
