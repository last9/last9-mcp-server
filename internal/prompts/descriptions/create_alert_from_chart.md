Create a disabled draft alert from a covered Last9 Discover chart using identity and editor fields from `describe_alert_chart`. There is no query, PromQL or expression parameter: use a returned catalog `signal_key` or explain the constraint; never call create_alert, create_kpi, get_entity_kpis, validate_alert_expression, or prometheus_range_query for this path.

Required: `surface` (`discover-service` or `discover-exceptions`), exact `chart_key`, scoped `service_name`, returned `signal_key`, and unique `name`. Optional `env` and `attributes` must use exactly the identity keys returned by `describe_alert_chart`.

`threshold` uses the signal's unit, must be 0 or more, and defaults to 0.01. `threshold_operator` is `>`, `<`, `>=`, `<=`, `==`, or `!=` (default `>`). `eval_window` is 1–60 minutes (default 5); `bad_minutes` is 1–`eval_window` (default 3). `severity` is `breach` or `threat`. Normally omit `group_id`; if supplied it must be the id returned for this chart. `description` stores responder context.

The rule is static and created disabled. Promote with `patch_alert is_disabled=false` only after confirmation. Score signals such as Apdex need an explicit 0..1 threshold; the 0.01 default suits rate/count signals.

Read `last9://reference/tool_recipes` for lifecycle and retry recipes. On success `_meta.reference_url` opens the saved rule; `_meta.links` labels it "Open alert" and adds "Open group" only when the API supplies a group name. On a timeout or connection error the create may or may not have been applied; check by name with get_alert_config (rule_name) before retrying.
