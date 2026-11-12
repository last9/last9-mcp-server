Preview an unsaved dashboard or a proposed patch without saving anything. Read-only: resolves variables, lints and executes supported queries, and returns per-panel results for review before approval.

## Input
- `dashboard`: full unsaved dashboard object (name, panels, variables, time). Omit when using `id`.
- `id`: saved dashboard ID; requires `ops` and excludes `dashboard`. Omit for a create preview.
- `ops`: ordered patch operations (add_panel, update_panel, remove_panel, move_panel, set_title, set_variable); no default. Required with `id`, forbidden with `dashboard`. Saved panels need distinct, nonempty IDs.
- `start_time_iso`, `end_time_iso`: required RFC3339 timestamps, no defaults; end must be after start, maximum window 24 hours.
- `variables`: optional scalar overrides; defaults to the first selected dashboard value, then the first available value, then built-in defaults. Range built-ins use the requested window; interval defaults are 1 minute, rate interval 5 minutes.

## Output and limits
`validation` summarizes definition and query checks, with `definition_error` for a create-schema mismatch; `panels` contain status `ok`, `empty`, or `error`, target diagnostics, resolved queries and bounded evidence. A valid non-query panel is `ok`; an invalid panel definition or any failed, unresolved or unsupported target makes its panel `error`. `saved` is always false. Only PromQL and LogJSON execute; other query types return errors, not successful empty results.

For a saved dashboard, `locked:false` confirms the API reported it unlocked. A locked dashboard instead returns a `dashboard is locked` recovery note; an absent lock state remains unknown and is omitted.

Patch `diff` lists net `panels_added`, `panels_changed` (including position changes), and `panels_removed`, with IDs and zero-based indices. Removed indices refer to the original dashboard; other indices refer to the preview. `changes` lists ordered operation changes; long before/after values may be shortened.

At most 50 panels, 100 queries and 100 ops; input and candidate dashboard each at most 65536 JSON bytes. Resolved queries over 65536 JSON bytes are rejected before execution. Execution has a 60-second deadline. Each target keeps at most 3 samples within 1024 JSON bytes; omitted samples set `truncated`. The JSON report is capped at 262144 bytes; oversized previews fail with guidance to reduce the input.

Client-side validation does not guarantee backend acceptance or rendering. Backend-assigned IDs, defaults, discarded fields and concurrent edits may change the eventual saved result. No dashboard write occurs, even when validation fails.
