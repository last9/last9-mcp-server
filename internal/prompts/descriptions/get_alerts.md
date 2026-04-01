# get_alerts Tool Arguments

## Purpose

Select the correct JSON arguments to call the `get_alerts` tool, which returns currently active or recently fired alerts from Last9.

## Parameters

- `time_iso` (string, optional): Evaluation time in RFC3339/ISO8601 format (e.g. `2026-02-09T15:04:05Z`). Use when the user specifies an explicit timestamp or datetime.
- `window` (integer, optional): Time window in **seconds** to look back for alerts. Default: 900. **Valid range: 1–3600** (maximum 1 hour). Do NOT exceed 3600.
- `lookback_minutes` (integer, optional): Alternative to `window`, in minutes. Valid range: 1–60. Used only when `window` is omitted.

## Critical constraint

The API enforces `window` between 1 and 3600 seconds. If a user asks for more than 1 hour of alerts, cap `window` at 3600. Do NOT generate `window` values like 5400, 7200, or 86400 — the API will reject them.

## Examples

User: "show me alerts from the last 30 minutes"
→ `{"window": 1800}`

User: "get alerts for the last hour"
→ `{"window": 3600}`

User: "show me alerts from the last 90 minutes"
→ `{"window": 3600}` (cap at max — do not use 5400)

User: "show alerts at 2026-03-23T11:00:00Z"
→ `{"time_iso": "2026-03-23T11:00:00Z", "window": 900}`

User: "show me active alerts right now"
→ `{}` (defaults apply)
