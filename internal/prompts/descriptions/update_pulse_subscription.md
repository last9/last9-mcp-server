Replace one Alert Hygiene Pulse subscription's configuration by `subscription_id`.

Read the current subscription first, then provide the complete replacement name, cron schedule, IANA timezone, recipient list, alert scope, measured analysis configuration, and configuration version. Never infer omitted values. Pass the subscription's current `version` (from `get_pulse_subscription`) as `expected_version`; a stale write returns HTTP 409 and must be re-read, not retried blindly. This write requires the server's explicit `pulse_manage` grant, user approval, and `confirmed: true`.
