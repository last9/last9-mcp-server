Build one evidence-preserving incident chronology from recorded Change Events and observed alert episodes.

Use this first for “what changed before/after this alert?”, incident ordering, mitigation timing, and affected-scope questions. The result preserves source timestamps, stable response IDs, source coverage, and deterministic temporal-proximity relationships. Proximity and matching labels are correlation only—never claim causality from this tool alone.

Inputs: either lookback_minutes (default/max 60) or both start_time_iso and end_time_iso (RFC3339, max one hour); optional service_name, env, alert_group_id, rule_id, event_name, kinds, and max_events (default 200, max 500).

Interpretation rules:
- “recorded change event,” not “the only change”;
- “first observed firing” and “last observed firing,” never inferred incident start/resolution;
- distinguish a complete empty source from failed/partial coverage;
- use recommended_follow_ups selectively for rule intent, deviations, logs, or traces;
- separate observations, hypotheses, and conclusions, citing timestamps and source systems.
