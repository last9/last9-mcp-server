List safe evidence projections for findings in one Alert Hygiene Pulse `run_id`.

Use `limit` from 1 to 100 and pass each opaque `next_cursor` back as `cursor` until empty when complete coverage is needed. Evidence is a protected safe projection and never includes retained raw payloads. Preserve `partial`, `failed`, and `truncated` state exactly.
