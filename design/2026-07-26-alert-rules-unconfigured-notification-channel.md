# Alert rules without notification channel — design spec

**Date:** 2026-07-26  
**Linear:** ENG-1512  
**Status:** Approved (brainstorming)  
**Branch context:** `fix/alert-channel-binding-report` / PR #191

## Problem

Customers need to list alert rules whose alert group has no per-entity notification channel binding — the same report as the dashboard Alert Groups filter `alertGroupChannelTypes=Not+configured`.

PR #191 exposed `service_fqid` on `get_notification_channels` and alert-group metadata on `get_alert_config`, but the workflow still requires an LLM to call two tools and join `entity_id` ↔ `service_fqid`. That is error-prone (semantics drift, missed joins, token cost) and produced contradictory description text (global channels as “covering all rules” vs dashboard semantics).

## Goal

Provide a single MCP tool that performs the join server-side and returns a ready-to-use audit list. Agents and automation should not need to cross-reference tools manually.

## Semantics (chosen: option C)

1. **Primary filter (dashboard parity):** A rule is included when no notification channel has `service_fqid` equal to the rule’s `entity_id`. Empty/`"-"` `service_fqid` (global/unbound channels) does not bind to any alert group.
2. **Global advisory:** When org-wide channels exist (`global: true` with non-empty `name` — skip API placeholder rows with empty names, consistent with `groupChannels.ts`), the response includes an explicit advisory that these channels do not satisfy per-alert-group binding under dashboard “Not configured” semantics. This prevents misreading “unconfigured” as “nobody gets notified anywhere.”

We do **not** implement operational mode (treat global channels as covering all rules). We do **not** add a separate parameter for semantics — option C is always applied.

## Recommended approach: dedicated tool

**Name:** `get_alert_rules_without_notification_channel`

**Why not a filter on `get_alert_config`:** Bloats an already large tool; easy for agents to miss unless heavily documented. **Why not summary-only TSV:** Less useful for follow-up (`get_entity_alert_rules`, severity/state context).

A partial handler stub exists in `internal/alerting/alert_rules_without_notification_channel.go` (unregistered). Implementation completes and wires that up.

## Behavior

### Data flow

1. `GET /notification_settings` → build `configuredEntityIDs` from channels with non-empty `service_fqid`.
2. Scan channels for global advisory: `global == true` and `strings.TrimSpace(name) != ""`. Collect name (and optionally type/id) for advisory line.
3. `GET /alert-rules` → apply optional filters (same set as `get_alert_config`).
4. Exclude rules where `configuredEntityIDs[rule.EntityID]`.
5. If any rules remain, `POST /entities/list` for enrichment (alert group name, data source, tags) and entity-backed filtering.
6. Format text response (no KPI/PromQL resolution — audit list, not investigation).

### Optional filters

Same as `get_alert_config`:

- `rule_id`, `search_term`, `rule_name`, `severity`, `rule_type`
- `alert_group_name`, `alert_group_type`, `data_source_name`, `tags`

Reuse validation (`rule_type` static/anomaly) and filter helpers from `alert_config.go`.

### Response format (text)

```
Global notification channels: 3 org-wide channel(s) configured (Mukta Email, Telegram-notif, …). These do not count as per-alert-group binding (dashboard "Not configured" semantics).

Found N alert rule(s) with no per-entity notification channel configured:

Alert Rule 1:
  ID: …
  Rule Name: …
  Entity ID: …
  Alert Group: …
  Data Source: … (when set)
  Tags: … (when set)
  State: …
  Severity: …
  …
```

When no global channels with names: `Global notification channels: none.`

Reuse `formatAlertConfigResponse` for rule bodies after the header/advisory block. Skip `resolveAlertConfigKPIs`.

### Deeplink

`deeplink.BuildAlertingGroupsLink()` (same as `get_alert_config`). Pre-filtered dashboard URL with `alertGroupChannelTypes=Not+configured` is out of scope for v1.

## Error handling

| Failure | Behavior |
|---------|----------|
| Notification settings API | Hard fail — join is essential for this tool |
| Alert rules API | Hard fail |
| Entity lookup + entity-backed filters active | Hard fail |
| Entity lookup + no entity-backed filters | Soft fail — return rules without Alert Group/Data Source/Tags enrichment |

## Documentation changes

1. Add `internal/prompts/descriptions/get_alert_rules_without_notification_channel.md`.
2. Embed in `internal/prompts/prompts.go`.
3. Register in `tools.go`.
4. Add to `alerts` toolset in `internal/toolsets/toolsets.go`.
5. **Remove** cross-reference recipes from `get_alert_config.md` and `get_notification_channels.md`; replace with one line pointing to this tool.
6. Do not duplicate long semantics in three places — the new tool description owns the recipe.

## Testing

### Unit tests

- Mock server: alert rules + notification settings + entities list.
- Cases: entity with bound channel excluded; unbound entity included; global channel triggers advisory but does not exclude rules; empty-name placeholder with `service_fqid` (match dashboard index behavior).
- Filter passthrough (severity, alert group name).
- Entity lookup soft-fail without entity filters.

**Dashboard alignment:** `AlertGroups/index.tsx` keys all channels by `service_fqid` for the Not configured map. For the configured entity set, use non-empty `service_fqid` only.

### Integration tests

- Live org: tool returns; header contains global advisory; rules lack per-entity binding.
- Replace `TestAlertChannelBindingReport_Integration` manual cross-reference with call to new tool.

### Verification commands

```bash
go test ./internal/alerting/... -count=1
go run . dump-tools | jq '.tools[] | select(.name=="get_alert_rules_without_notification_channel")'
```

## Out of scope (v1)

- Operational semantics (global channels clear all rules).
- Alert-group-only report without rules.
- Dashboard deep link with pre-applied Not configured filter.
- KPI/PromQL resolution on this tool.
- Shared `AlertConfigFilterArgs` refactor across handlers.

## Success criteria

1. Agent can answer “which alert rules have no notification channel configured?” with **one tool call**.
2. Output matches dashboard Not configured filter for per-entity binding.
3. Global channels are surfaced explicitly so results are not misinterpreted.
4. `get_alert_config` / `get_notification_channels` descriptions no longer teach LLM join logic.
