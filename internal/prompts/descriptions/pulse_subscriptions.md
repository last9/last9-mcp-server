Manage Alert Hygiene Pulse subscription configuration through canonical organization-scoped APIs.

Actions: list subscriptions (`list_pulse_subscriptions`), get a subscription (`get_pulse_subscription`), create a disabled subscription (`create_pulse_subscription`), update a subscription (`update_pulse_subscription`), enable it (`enable_pulse_subscription`), or disable it (`disable_pulse_subscription`).

Read the current subscription before proposing a write. Creation is always disabled; configuration and enablement are separate actions. Never infer scope, schedule, timezone, recipients, thresholds, or configuration versions. Write tools require the server's explicit `pulse_manage` grant and `confirmed: true` after user approval.
