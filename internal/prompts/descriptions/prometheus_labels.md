
	List label names in the selected datasource and time window.
	This works similar to the prometheus /labels call
	It returns a JSON object whose `labels` field is an array of label names.
	Parameters:
	- match_query: Optional series selector, for example `up`. Omit to list label names across all series in the window.
	- match: Alias for match_query. Prefer match_query when supplying a selector.
	- lookback_minutes: (Optional) Number of minutes to look back from now. Defaults to 60. When no window is given, match_query names one metric and the 60 minutes are empty (daily or sparse metrics), the server retries once over 4 days and adds a text block saying so.
	- start_time_iso: (Optional) Start time of the time range in RFC3339/ISO8601 format (e.g. 2026-02-09T15:04:05Z). Overrides lookback when provided.
	- end_time_iso: (Optional) End time of the time range in RFC3339/ISO8601 format (e.g. 2026-02-09T16:04:05Z). Defaults to current time.
	- datasource: (Optional) Datasource name; omitted uses the default, a typo its closest match; looser or ambiguous names list candidates, no query. An additional block names it unless the name was exact.

	Leave the window unset unless you know the metric's cadence, so a sparse metric gets the automatic 4-day retry.
