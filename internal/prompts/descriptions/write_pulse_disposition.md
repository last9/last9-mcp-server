Write one optimistic-concurrency Alert Hygiene Pulse disposition: `tune`, `expected`, or `investigate`.

Action: write a disposition (`write_pulse_disposition`).

Read the finding first and pass its current `disposition_version` as `expected_version`. Ask the user before writing and set `confirmed: true` only after approval. A stale write returns HTTP 409 and must be re-read, not retried blindly. `investigate` records intent only; this tool never launches an investigation or mutates an alert rule.
