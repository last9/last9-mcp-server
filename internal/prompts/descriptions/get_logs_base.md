
	Get logs for service or group of services using JSON pipeline queries for advanced filtering, parsing, aggregation, and processing. 
	
	This tool requires the logjson_query parameter which contains a JSON pipeline query. Use the logjson_query_builder prompt to generate these queries from natural language descriptions.

	Time format rules:
	- Prefer lookback_minutes for relative windows (for example, last 5 or 60 minutes).
	- Use start_time_iso and end_time_iso for absolute windows.
	- start_time_iso/end_time_iso accept RFC3339/ISO8601 (e.g. 2026-02-09T15:04:05Z).
	- Legacy format YYYY-MM-DD HH:MM:SS is accepted only for compatibility.
	- If both lookback_minutes and absolute times are provided, absolute times take precedence.

	Parameters:
	- logjson_query: (Required) JSON pipeline query array for advanced log filtering and processing. Use logjson_query_builder prompt to generate from natural language.
	- lookback_minutes: (Optional) Number of minutes to look back from now. Default: 5 minutes.
	- start_time_iso: (Optional) Start time in RFC3339/ISO8601 format (e.g. 2026-02-09T15:04:05Z). Leave empty to use lookback_minutes.
	- end_time_iso: (Optional) End time in RFC3339/ISO8601 format (e.g. 2026-02-09T16:04:05Z). Leave empty to default to current time.
	- index: (Optional) Explicit log index to query. Accepted values are physical_index:<name> and rehydration_index:<block_name>. Omit it when the user did not specify an index.

	Field reference rules:
	- Use ServiceName for service filters/grouping. Do not use bare service.name.
	- Use attributes['field'] for log attributes.
	- Use resources['field'] for resource attributes such as Kubernetes metadata.
	- Bare dotted field references are rejected unless they are normalized aliases like service.name or k8s.*.

	The logjson_query supports:
	- Filter operations: Filter logs based on conditions
	- Parse operations: Parse log content (json, regexp, logfmt)
	- Aggregate operations: Perform aggregations (sum, avg, count, etc.)
	- Window aggregate operations: Time-windowed aggregations
	- Transform operations: Transform/extract fields
	- Select operations: Select specific fields and apply limits

	Response contains the results of the JSON pipeline query execution.

