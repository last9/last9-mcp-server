Apply targeted edits to an existing custom dashboard by ID and get back a compact diff of what changed, not the full dashboard. Prefer this over update_dashboard for small refinements: unchanged panels are never re-sent. Use create_dashboard only for a net-new dashboard; once it has an id, refine it here or with update_dashboard.

`ops` run in order against the current dashboard; if any op is invalid, nothing is written and the error names it as `ops[i]`. Panel ids come from get_dashboard.
- `add_panel`: `panel` (full panel object, same shape as create_dashboard; `version` defaults to 1), optional `index` (0-based insert position, default append).
- `update_panel`: `panel_id`, `fields` (top-level panel keys to replace, e.g. `name`, `queries`, `layout`, `visualization`; `null` removes a key; `id` cannot change).
- `remove_panel`: `panel_id`.
- `move_panel`: `panel_id`, `index` (0-based position in `panels` after the move).
- `set_title`: `title` (the dashboard name).
- `set_variable`: `variable` object; replaces the variable with the same `target`, otherwise appends it.

The tool reads the dashboard, applies the ops and writes it back, so an edit made elsewhere between those two calls is overwritten. Ops the dashboard already satisfies are left out of `changes`; if no op changes anything, nothing is written. After writing, the tool re-reads the dashboard: panel `fields` show the stored values, and `not_stored` lists keys the API dropped (it keeps only panel keys it knows) and `not_removed` keys a `null` did not remove (the API restored a default), so those edits did not land. Diff values longer than 200 characters are shortened but keep their JSON type: strings are cut, and trailing array items or object keys are dropped, marked with `…`. Readonly system dashboards return an error.
