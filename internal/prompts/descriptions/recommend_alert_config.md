Recommend a safe, actionable static or adaptive alert design before creating it. Produces lifecycle guidance, validation gates, and responder-context requirements without mutating Last9. Use telemetry and discovery tools to ground thresholds, sensitivity, labels, and entity/KPI IDs; never treat the recommendation as evidence by itself. rule_type static means a threshold rule (condition, alert_condition, and eval_window required); anomaly means an adaptive rule with expression adaptive_std_cmp(<primary_indicator>, sensitivity, window), where condition, alert_condition, and eval_window are optional and default to "expr > 0", "count_true(result) >= 1", and 1. Rules are always created disabled. Grammar, units and static/adaptive templates for latency, error rate and saturation: read last9://reference/alerting.

Parameters (only these; any other argument is rejected):
- signal (required): metric or user-impact signal to alert on, including its unit and relevant dimensions.
- objective (required): operational symptom or user impact the alert should detect.
- rule_type (optional): "static" or "anomaly". Default "static".
- severity (optional): "breach" or "threat". Default "breach".
- environment (optional): environment and traffic profile, for example production with weekday seasonality. Default: not supplied.
