Create an alert rule directly from a covered Last9 Discover chart in one call, using the chart identity plus flat editor fields you already have from describe_alert_chart. Chart-derived rules are always static-threshold rules: there is no algorithm parameter on this tool and none can be set. Lifecycle: the rule is created enabled and fires immediately once its condition is met — unlike create_alert, which creates disabled for validation and requires promoting with patch_alert.

Parameters:
- surface (Required): Surface identifier; one of discover-service or discover-exceptions.
- chart_key (Required): Chart key within the surface, exactly as describe_alert_chart reports it (for example apdex, error_rate, response_time, exception_count).
- service_name (Required): Service/entity name the chart is scoped to.
- signal_key (Required): Signal key returned by describe_alert_chart for this chart; never guess keys.
- name (Required): Alert rule name; duplicate names are rejected upstream with a conflict.
- env: Environment when the chart distinguishes one, for example prod; omit otherwise.
- attributes: Extra identity dimensions some charts need; use exactly the key names describe_alert_chart reports.
- threshold: Breach threshold in the signal's own unit as reported by describe_alert_chart; omit for the backend default 0.01.
- threshold_operator: One of > < >= <= == !=; default >.
- eval_window: Evaluation window in minutes, range 1-60; default 5.
- bad_minutes: Minutes within eval_window the condition must hold before firing, range 1-eval_window; default 3.
- severity: breach or threat; when omitted the backend derives it from the algorithm, and static chart rules default to breach.

Units warning: the defaults (> 0.01 over a 5-minute window with 3 bad minutes) fit rate/count-style signals such as error_rate or exception_count. Score-unit signals like apdex live on a 0..1 scale — an explicit threshold there is required, because the 0.01 default would fire constantly.

Retry discipline: after a timeout never retry blindly — first verify whether the rule exists via get_entity_alert_rules, since a timed-out create may still have succeeded and a blind retry duplicates it. On a duplicate-name conflict, check existing rules and pick a different name.

Credentials must clear the alert-intelligence route's POST gate; viewer-role tokens are rejected with a permissions error.