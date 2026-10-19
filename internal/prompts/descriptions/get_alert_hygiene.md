Audit alert rules for hygiene problems over the last 7 or 30 days and suggest a fix for each.

Read-only: this tool never changes anything. Present fixes to the user; apply one in the Last9 alerting UI
only once the user explicitly confirms it.

Parameters:
- window: `7d` or `30d` (default `7d`).
- limit: maximum flagged rules to return, worst first (default 20, range 1-100).

Categories (a rule may have several; disabled rules are never flagged):
- noisy: fires at least 5 times/day with under 20% of episodes acked, or muted for at least 25%
  of the observed span. With null `ack` (not recorded yet) it is judged on fire rate alone and
  the reason says so.
- flapping: more than 4 flaps/day.
- stale: has not fired in the window. A rule that never fired (null `last_fired_at`, zero
  `fire_count`) counts only when its history covers the whole window.
- unrouted: its alert group has no notification channel.

Alert history is kept about 21 days, so `from` is where covered history starts, `covered_days`
its length; `partial_window` is true when shorter than `window`. Rates use the span from
`from`, or `history_since` if later, at least one day.
A null field means not recorded, never zero or false: null `muted_seconds` skips noisy-by-mute,
null `routed` skips unrouted, null `history_since` skips never-fired stale. `null_fields`
counts enabled rules per null field.

Suggested fixes (`suggested_fix`, sometimes `alternative_fix`, each `{parameter, current, proposed}`):
- flapping, and noisy non-static rules: raise `bad_minutes`, the N in
  `alert_condition = count_true(result) >= N`, up to `eval_window`; or lengthen `eval_window`
  (minutes).
- noisy static threshold: raise (or, for `<`, lower) the threshold in `condition` (`expr > 100`),
  or switch `expression` to the `adaptive_std_cmp(metric, sensitivity, window)` macro (an
  expression, not an `algorithm` value).
- unrouted: attach a notification channel to the alert group.
- stale: set `is_disabled` to true, or delete the rule, once confirmed unneeded.

Output is compact JSON: `{window, from, to, covered_days, partial_window, rules_evaluated, flagged_rules, truncated, null_fields, rules}`.
Each rule has `findings` (`category`, `reason` with the numbers, `suggested_fix`) and `stats`,
the raw per-rule API data (timestamps in unix seconds).

Ranked by number of categories, then fire count, then flaps/day. For a rule's
firing timeline use get_alert_rule_state; for its full configuration use get_alert_config.
