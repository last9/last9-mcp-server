Answers "what am I not alerting on?": lists alert coverage gaps for a service, an
environment, or one alert group, or for the whole organization when no scope is given.
Read-only: it never creates, changes, or unmutes anything.

Gap kinds:
- uncovered_signal: an entity has no enabled alert rule on a golden signal (latency,
  error_rate, throughput, or saturation). When a disabled rule would cover it, the gap
  names that rule and suggests re-enabling it.
- all_rules_disabled: an alert group has rules, but every one of them is disabled.
- no_notification_channel: a rule whose severity no notification channel on its alert
  group routes, so it fires without notifying anyone.
- muted_too_long: a rule muted indefinitely, or for more than muted_days days.

Optional parameters (values are trimmed; blank strings are ignored):
- service: Service name.
- environment: Deployment environment (for example prod).
- entity_id: Alert group (entity) UUID, as returned by get_alert_groups.
- muted_days: Whole days, 1 to 3650. Default: 7. A rule muted longer than this is a
  muted_too_long gap.

Output is compact JSON {scope, generated_at, complete, summary, counts, groups}:
- groups: one entry per gap kind present, as {kind, count, gaps}, in the order above.
- Each gap: entity_name, service, rule_name, severity, detail, muted_since and
  mute_until (RFC3339 UTC; muted_since is the rule's last update, since the mute start
  is not recorded; mute_until is omitted for an indefinite mute),
  suggested_tool, and suggested_args (whichever of entity_id, rule_id, signal apply).
- counts: the API's total per gap kind.
- complete: true only when there are no gaps in the scope; summary then says coverage
  is complete for that scope.

suggested_tool names the tool that would close the gap: create_alert,
create_alert_from_chart, attach_notification_channel, or patch_alert. Call it only
if available in this session and only after the user confirms the change; otherwise
describe the fix for the Last9 alerting UI.
