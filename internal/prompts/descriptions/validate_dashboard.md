Read-only validation of a Last9 dashboard: lints and executes every panel query in a bounded time window and classifies each result (data / no data / invalid / error). Accepts exactly one of dashboard_id (saved dashboard) or dashboard_definition (unsaved inline definition — a true dry run, nothing is persisted). Window must be ≤ 24h. Never mutates anything.

Day-1 note: empty results classify as valid_no_data with diagnosis null; evidence-backed empty-result probes are a follow-up.
