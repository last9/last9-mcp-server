
	Return the environments available for the services. This tool returns an array of environments. These env can act as
	label or argument values for other tools.
	Parameters:
	- service: (Optional) Service name to filter environments for (e.g. my-api). When omitted, returns environments across all services.
	- lookback_minutes: (Optional) Number of minutes to look back from now. Defaults to 60.
	- start_time_iso: (Optional) Start time of the time range in RFC3339/ISO8601 format (e.g. 2026-02-09T15:04:05Z). Overrides lookback when provided.
	- end_time_iso: (Optional) End time of the time range in RFC3339/ISO8601 format (e.g. 2026-02-09T16:04:05Z). Defaults to current time.

	Returns an array of environments.
