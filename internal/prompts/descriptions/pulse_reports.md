Read canonical Alert Hygiene Pulse run status, report, findings, finding history, source coverage, and safe evidence.

Actions: list run status (`list_pulse_runs`), get run status (`get_pulse_run`), get a report (`get_pulse_report`), list findings (`list_pulse_findings`), get finding detail (`get_pulse_finding`), or list safe evidence (`list_pulse_evidence`).

Follow opaque `next_cursor` values until empty when complete coverage is needed. Preserve `partial`, `failed`, `truncated`, analysis status, and delivery status exactly; delivery failure does not change analysis truth. Evidence is a protected safe projection and never includes retained raw payloads.
