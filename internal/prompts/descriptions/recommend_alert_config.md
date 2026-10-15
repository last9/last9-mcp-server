Recommend a safe, actionable static or supported anomaly-macro alert design before creating it. Produces lifecycle guidance, validation gates, and responder-context requirements without mutating Last9. Use telemetry and discovery tools to ground thresholds, sensitivity, labels, and entity/KPI IDs; never treat the recommendation as evidence by itself. Adaptive expressions are saved as adaptive rules; condition, alert_condition, and eval_window are optional for them, so supplied values are kept and only omitted ones get defaults.

Parameters (only these; any other argument is rejected):
- signal (required): metric or user-impact signal to alert on, including its unit and relevant dimensions.
- objective (required): operational symptom or user impact the alert should detect.
- rule_type (optional): "static" or "anomaly". Default "static".
- severity (optional): "breach" or "threat". Default "breach".
- environment (optional): environment and traffic profile, for example production with weekday seasonality. Default: not supplied.
