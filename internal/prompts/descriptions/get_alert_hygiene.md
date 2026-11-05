Audit alert rules for hygiene problems over 7 or 30 days, suggesting a fix for each.

Read-only: it never changes anything. Present each fix as a proposal; apply it only after the
user explicitly confirms it.

Parameters: window `7d` or `30d` (default `7d`, ending now); limit, flagged rules returned worst
first (default 20, range 1-100).

Categories (a rule may have several; disabled rules are never flagged):
- noisy: fires at least 5 times/day with under 20% of episodes acked. With `ack_rate`
  unavailable it is judged on fire rate alone and the reason says so. Mute time is not reported.
- flapping: more than 4 flaps/day.
- stale: last fired before the window with no firing time in it, or a configured rule with no
  episode in it while `coverage.lifecycle` is complete and the list is not truncated.
- unrouted: no firing reached a channel and its alert group has no channel attached.

Stats: `{status, count|value, reason}` or null; unavailable values never read as zero and
skip the check that needs them. `unavailable` counts enabled rules per missing value (`routed`
also if attachments were unreadable), plus `rule_config` (not in the alert rules list; fixes
carry no current value) and `activity` (no episode, silence unproven: not assessed).
`coverage`: how much of the window history covered; rates use that span, at least a day.
`upstream_truncated`: the API returned only its 200 loudest rules.

Suggested fixes (`suggested_fix`, sometimes `alternative_fix`, each `{parameter, current, proposed}`):
- flapping, and noisy non-static rules: raise `bad_minutes` (N in
  `alert_condition = count_true(result) >= N`) up to `eval_window`, or lengthen `eval_window`
  (minutes, max 60).
- noisy static threshold: raise (or, for `<`, lower) the threshold in `condition` (`expr > 100`),
  or switch `expression` to the `adaptive_std_cmp(metric, sensitivity, window)` macro (not an
  `algorithm` value).
- unrouted: attach a notification channel.
- stale: set `is_disabled` to true, or delete it, once confirmed unneeded.

Compact JSON output: `{window, from, to, coverage, upstream_truncated, rules_evaluated, flagged_rules, truncated, omitted_for_size, unavailable, rules}`.
`truncated`: more flagged rules than `limit` or the 32 KB target holds (`omitted_for_size` counts size cuts).
Each rule has `findings` (`category`, `reason` with the numbers, `suggested_fix`) and raw `stats`.

Ranked by category count, then fire count, then flaps/day.

## Pairs well with
- `get_alert_rule_state` (a rule's firing timeline)
- `get_alert_config` (a rule's full configuration)
- `update_alert` (apply a confirmed tuning fix)
- `patch_alert` (apply a confirmed is_disabled fix)
- `delete_alert` (delete a confirmed stale rule)
- `attach_notification_channel` (route a confirmed unrouted group)
