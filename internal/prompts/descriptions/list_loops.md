List Loops in the organization: id, name, status, enabled, action_mode, and last_run.

Returns `{"loops": [...], "count"}`. Optional `query` is a case-insensitive substring of name or id. Use `get_loop` for graph summary and setup gaps. When Loops is not enabled, returns an actionable error naming `LOOPS_NOT_ENABLED`.