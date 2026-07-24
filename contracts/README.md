# Investigation contracts

These artifacts are the repository-neutral compatibility fixtures for ENG-1488.

- `investigation-evidence-v1.schema.json` defines the shared evidence envelope.
- `investigation-workflow-v1.schema.json` defines supervisor state and events.
- `feature-flags-v1.json` reserves default-off rollout controls.
- `fixtures/` contains deterministic examples consumed by compatibility tests.

Compatibility rules:

1. Producers emit exact major versions `investigation-evidence/v1` and `investigation-workflow/v1`.
2. Consumers reject unknown major versions and ignore unknown optional fields within v1.
3. Times use RFC3339, windows are half-open `[start,end)`, and public durations use milliseconds.
4. Evidence is immutable. Workflow references use an evidence ID and SHA-256 content hash.
5. Events are append-only and strictly increase `sequence` within an investigation.
6. Partial, truncated, warning, limitation, and provenance metadata may not be discarded by adapters.

These schemas are additive foundations, not a production endpoint or supervisor implementation.
