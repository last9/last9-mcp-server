Discover Last9 alert rules and their metadata. Use rule_id for an exact rule, typed filters for known fields, or search_term for a case-insensitive search across rule and alert-group metadata. Use the returned entity_id with get_entity_alert_rules when resolved PromQL or expression logic is needed.

Filters are described in the input schema. Important combination rules:
- tags use AND semantics;
- only_without_notification_channel and notification_channel_types use OR semantics;
- notification channel type/name/severity filters must match the same per-entity binding row;
- global org-wide channels do not satisfy per-entity channel filters.

Each rule includes identity, name, primary indicator, entity/alert-group metadata, datasource, tags, state, severity, algorithm, timestamps, and notification-channel status. Notification Channel Bindings contains each per-entity binding's type, name, severity, and snooze/in_use flags when available. "Not configured" means no per-entity binding, not necessarily no global channel.
