Not for Discover charts: even with pasted PromQL, use `create_alert_from_chart` with a catalog `signal_key`; do not rebuild its query. Create a disabled draft rule in an existing alert group. Create once, keep the returned id, and refine with `patch_alert` or `update_alert`.

Pass `entity_id` separately and the complete typed body in `alert_rule`. Bind `primary_indicator` and `expression_args` to KPIs from `get_entity_kpis` (or create one with `create_kpi`). Each expression key is the KPI's exact name. A `rule_name` the group already uses (case-insensitive) is refused.

Static rules use arithmetic over indicators; `condition`, `alert_condition`, and `eval_window` (0–60 minutes) are required. Adaptive expressions are exactly `adaptive_std_cmp(<primary_indicator>, sensitivity, window)`; omitted condition fields default to `expr > 0`, `count_true(result) >= 1`, and 1 minute. The backend derives algorithm.

Store responder context in `properties` (`description`, runbook link, annotations, `query_mode`). Notification routing belongs to the group. Validate expressions before saving and promote with `patch_alert is_disabled=false` only after confirmation. For a custom dashboard panel, create one from the panel's PromQL with create_kpi; Discover charts may not.

Set `on_no_data: "alert"` when missing samples should fire a distinct No data alert after 2x `eval_window` minutes, including when the metric has never reported since the rule was enabled. Omit it to leave this off. The disabled draft cannot alert; the timer starts when it is enabled. Do not use it for event-style metrics where silence is normal or queries containing `or vector(0)`, which never look absent. After data returns, resolution takes about 11 minutes.

Read `last9://reference/alerting` for grammar, units, the complete field matrix, and templates; read `last9://reference/tool_recipes` for the authoring lifecycle.

On success `_meta.reference_url` opens the saved rule; `_meta.links` labels it "Open alert" and adds "Open group" only when the API supplies a group name. On a timeout or connection error the create may or may not have been applied; check by name with get_alert_config (rule_name) before retrying.
