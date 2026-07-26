# Alert rules without notification channel — design spec

**Date:** 2026-07-26  
**Linear:** ENG-1512  
**Status:** Implemented via filter param (revised from dedicated tool)  
**Branch context:** `fix/alert-channel-binding-report` / PR #191

## Problem

Customers need alert rules whose alert group has no per-entity notification channel binding — same as dashboard `alertGroupChannelTypes=Not+configured`. Exposing `service_fqid` and `entity_id` in separate tools still required LLM-side joins.

## Decision: filter param on `get_alert_config` (not a new tool)

**Parameter:** `only_without_notification_channel` (boolean)

**Why:** Same server-side correctness as a dedicated tool, no extra `tools/list` entry, discoverable via `get_alert_config` description. Trade-off: easier to miss than a dedicated tool name; mitigated by clear param description and optional filters list entry.

## Semantics (option C)

1. **Filter:** Include rules only when no channel has non-empty `service_fqid` equal to `entity_id`.
2. **Global advisory:** Response prefixes a line listing org-wide `global: true` channels (non-empty `name`). States they do not satisfy per-alert-group binding under dashboard "Not configured".

## Behavior

1. When `only_without_notification_channel` is true: fetch `/notification_settings`, build configured entity set, format global advisory.
2. Fetch `/alert-rules`, apply standard optional filters, then exclude rules on configured entities.
3. Entity enrichment + entity-backed filters (same soft/hard fail as plain `get_alert_config`).
4. Skip KPI/PromQL resolution when filter is active (audit list).
5. Custom response header: "Found N alert rule(s) with no per-entity notification channel configured".

## Documentation

- `get_alert_config.md`: documents the filter param; removed LLM cross-reference recipe from `entity_id` bullet.
- `get_notification_channels.md`: removed cross-reference recipe; TSV column docs only.

## Testing

- Unit: `TestGetAlertConfigHandler_OnlyWithoutNotificationChannel`
- Integration: `TestGetAlertConfigHandler_Integration_OnlyWithoutNotificationChannel`

## Success criteria

1. One tool call with `only_without_notification_channel: true` produces the audit list.
2. Dashboard Not configured semantics encoded in server.
3. Global channels surfaced in advisory line.
4. No join recipe in other tool descriptions.
