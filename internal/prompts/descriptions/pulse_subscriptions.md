Manage Alert Hygiene Pulse subscription configuration through canonical organization-scoped APIs.

Read the current subscription before proposing a write. Creation is always disabled; configuration and enablement are separate actions. Never infer scope, schedule, timezone, recipients, thresholds, or configuration versions. Write tools require the server's explicit `pulse_manage` grant and `confirmed: true` after user approval.
