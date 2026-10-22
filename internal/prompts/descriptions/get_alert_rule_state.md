Gets the historical firing state of alert rules over a specified time range, grouped by rule_id.
It samples alert state at each step within the time range and returns 1 for firing and 0 otherwise at each timestamp.

Required parameters:
- start_time: start of the range (inclusive), Unix epoch seconds (e.g. 1718000000) or RFC3339 (e.g. 2024-06-10T06:13:20Z); epoch milliseconds, microseconds and nanoseconds are converted to seconds
- end_time: end of the range (inclusive), same formats
- step: Resolution in seconds between samples

Optional filters forwarded to the upstream API (no client-side filtering is applied):
- alert_group_id: Filter by alert group ID
- rule_name: Regex filter on rule name
- alert_group_name: Regex filter on alert group name
- label_filters: Comma-separated key=value label filters
- state: Filter by state (e.g. firing)

Output is a JSON map of rule_id -> [{timestamp, is_firing}]. A timestamp at which a rule is absent from the upstream
response is reported as is_firing=0; this reflects "not observed as firing" and not necessarily a confirmed normal state.
The number of samples ((end_time - start_time) / step + 1) is capped at 100.

## When to use
Whether alert rules were firing at each step of a past range.

## When not to use
- Alerts firing now: `get_alerts`
- Rule configuration: `get_alert_config`

## Pairs well with
- `get_current_time` (epoch bounds)
- `get_alert_config` (rule ids and names)

## Output shape
JSON map of rule_id to `[{timestamp, is_firing}]`.

## Known traps
`start_time` and `end_time` are absolute times, never durations or offsets like `now-1h`; values before 2000 are rejected. At most 100 samples per call.
