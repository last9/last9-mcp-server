Audit alert rules for hygiene problems over 7 or 30 days, suggesting a fix for each.

Read-only: it never changes anything. Present each fix as a proposal; apply it only after the
user explicitly confirms it.

Parameters: window `7d` or `30d` (default `7d`, ending now); limit, flagged rules returned worst
first (default 20, range 1-100).

Categories (a rule may have several; disabled rules are never flagged):
- noisy: fires at least 5 times/day with under 20% of episodes acked. With an unavailable
  `ack_rate` it is judged on fire rate alone and the reason says so. Mute time is not reported.
- flapping: more than 4 flaps/day.
- stale: last fired before the window with no firing time in it, or no recorded fire while
  `coverage.lifecycle` is complete.
- unrouted: its alert group has no notification channel.

Each stat is `{status, count|value, reason}`; an unavailable value is never read as zero and
skips the check that needs it. `unavailable` counts enabled rules per missing value, including
`rule_config` (rule not in the alert rules list, so its fixes carry no current value).
`coverage` says how much of the window alert history covered; rates use the covered span, at
least one day. `upstream_truncated`: the API returned only its loudest rules (up to 200 by fire
count), so quieter rules, the likeliest stale ones, were not evaluated.

Suggested fixes (`suggested_fix`, sometimes `alternative_fix`, each `{parameter, current, proposed}`):
- flapping, and noisy non-static rules: raise `bad_minutes` (N in
  `alert_condition = count_true(result) >= N`) up to `eval_window`, or lengthen `eval_window`
  (minutes, max 60).
- noisy static threshold: raise (or, for `<`, lower) the threshold in `condition` (`expr > 100`),
  or switch `expression` to the `adaptive_std_cmp(metric, sensitivity, window)` macro (an
  expression, not an `algorithm` value).
- unrouted: attach a notification channel to the alert group.
- stale: set `is_disabled` to true, or delete the rule, once confirmed unneeded.

Output is compact JSON: `{window, from, to, coverage, upstream_truncated, rules_evaluated, flagged_rules, truncated, omitted_for_size, unavailable, rules}`.
`truncated`: more flagged rules than `limit` or 32 KB holds (`omitted_for_size` counts size cuts).
Each rule has `findings` (`category`, `reason` with the numbers, `suggested_fix`) and `stats`,
the raw per-rule API data.

Ranked by number of categories, then fire count, then flaps/day.

## Pairs well with
- `get_alert_rule_state` (a rule's firing timeline)
- `get_alert_config` (a rule's full configuration)
- `update_alert` (apply a confirmed tuning fix)
- `patch_alert` (apply a confirmed is_disabled fix)
- `delete_alert` (delete a confirmed stale rule)
- `attach_notification_channel` (route a confirmed unrouted group)
