Gets the historical firing state of alert rules over a specified time range, grouped by rule_id.
It polls the /alerts/monitor API at each step within the time range and returns 1 for firing and 0 otherwise at each timestamp.

Required parameters:
- start_time: Unix epoch start of the range (inclusive)
- end_time: Unix epoch end of the range (inclusive)
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