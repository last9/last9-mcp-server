Write tool: locks or unlocks a custom dashboard by ID. Call it only when the user explicitly asks to lock or unlock a dashboard.

- `id` (required): dashboard UUID from `list_dashboards`.
- `locked` (required): `true` locks the dashboard, `false` unlocks it.

A locked dashboard refuses `update_dashboard`, `patch_dashboard` and `delete_dashboard` with an error starting `dashboard is locked`. Only its author or an org admin can lock or unlock it; built-in dashboards are always read-only and cannot be locked or unlocked.

Returns `{"id", "locked"}` with the resulting state and the dashboard link in `reference_url`. `get_dashboard` shows the current `locked` state and `created_by`.