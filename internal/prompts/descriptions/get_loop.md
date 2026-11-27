Get one Loop by `loop_id`: status fields, setup gaps, and a redacted graph summary (no tokens or webhook secrets).

Returns id, name, status, enabled, action_mode, last_run, setup_gaps, and graph_summary. Use after create or before lifecycle changes. When Loops is not enabled, returns an actionable error naming `LOOPS_NOT_ENABLED`.