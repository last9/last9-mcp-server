List Alert Hygiene Pulse findings for one `run_id`.

Use `limit` from 1 to 100 and pass each opaque `next_cursor` back as `cursor` until empty when complete coverage is needed. Preserve each finding's identifiers and status exactly; use its `id` field as the occurrence identifier for `get_pulse_finding`.
