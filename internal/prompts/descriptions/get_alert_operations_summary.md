On-call health over a window: alerts raised vs acknowledged, unacknowledged, current
backlog, notification deliveries, noisiest rules and what needs attention. Use it for
"how many alerts fired vs were acked last week", "which rules are noisiest, flapping or
not routed". For alerts firing right now use get_alerts; for rule configuration use
get_alert_config.

Parameters:
- start_time_iso, end_time_iso (required): RFC3339 window; at most 31 days. Call
  get_current_time first for relative windows ("last 7 days").
- timezone: IANA name for day boundaries (default: UTC).
- severities, services: lists that narrow the summary (default: all).
- include_rules (default: false): also return per-rule hygiene in rules:
  top_by_fire_count (top 20), flapping (flaps_per_day >= 1), unrouted (routed=false),
  each capped at 20, with total_rules, flapping_total and unrouted_total.
  Per-rule fields: fire_count, flaps_per_day, firing_seconds, ack.ack_rate (0-1),
  ack.median_seconds_to_ack (seconds), routed; null means no history recorded.

Output: JSON with window, alerts_raised, acknowledged, unacknowledged, current_backlog,
delivered{total,slack,email}, notification_outcomes, top_rules, needs_attention,
coverage, rules (null unless include_rules) and errors (only when part of it failed).

Every metric is {status, count?, reason?}:
- available: count is complete for the window.
- partial: count covers only part of the window. It is NOT a lower bound or an
  estimate; always report it with "partial" and the reason, never as a bare number.
- unavailable: no count; say it is unavailable and give the reason.
Use coverage to say where data is incomplete, e.g. "lifecycle data starts on <date>".
Never compute a ratio (ack rate, raised minus acked) from a partial or unavailable metric.
If rules is null with an errors entry, the summary is still valid; say hygiene failed.
