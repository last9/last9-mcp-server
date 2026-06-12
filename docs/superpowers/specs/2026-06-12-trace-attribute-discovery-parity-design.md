# Trace Attribute Discovery Parity (ENG-1250)

**Status:** Design approved, pending spec review
**Ticket:** [ENG-1250](https://linear.app/last9/issue/ENG-1250)
**Related:** ENG-1244 (logs root cause), last9-mcp-server#160 (logs implementation)

## Problem

The MCP server's trace attribute discovery is weaker than its logs equivalent. After PR #160, logs expose two complementary tools:

- `get_log_attributes` — the **global** label catalog (`GET /logs/api/v1/labels`).
- `get_log_attributes_for_pipeline` — fields **actually present after a pipeline filter** (`POST /logs/api/v2/series/json`), each enriched with a ready-to-use `filter_field`.

This two-step flow exists because an LLM that guesses a `filter_field` from the global catalog can pick a key that is empty for its actual scope — the failure mode behind ENG-1244 (status keyed `status_code` on some sources, `http.status_code` on others).

Traces have only one attributes tool, `get_trace_attributes`, and it is neither truly global nor scoped: it calls the series endpoint with a **hardcoded empty pipeline**. That is the exact approach the logs code comments warn returns only a subset. There is no pipeline-scoped trace tool at all.

## Goal

Bring traces to full parity with the logs flow:

1. Add a pipeline-scoped trace attributes tool.
2. Move the global trace attributes tool to the dashboard's real global endpoint.
3. Make trace attribute-value lookups pipeline-aware.

Non-goal: changing `get_traces` / `get_service_traces` query behavior, or any frontend change.

## Current state (verified)

| Tool | Endpoint today | Pipeline support |
|------|----------------|------------------|
| `get_trace_attributes` | `POST /cat/api/traces/v2/series/json`, empty pipeline | none (hardcoded empty) |
| `get_trace_attribute_values` | `POST /cat/api/traces/v2/label/json/{tag}/values`, empty pipeline | none (hardcoded empty) |
| `get_log_attributes` | `GET /logs/api/v1/labels` | n/a (global) |
| `get_log_attributes_for_pipeline` | `POST /logs/api/v2/series/json` | required |

**Frontend reference** (`dashboard/app/src/App/scenes/Traces/`): `traceHooks.useFetchTags` toggles on `useSeries` between global `GET /cat/api/search/tags` (`getTraceTags`) and scoped `POST /cat/api/traces/v2/series/json` with pipeline (`getTraceTagsSeriesJson`). `useFetchTagValues` toggles on `useJson` between global and pipeline-scoped values.

**Enrichment** (`internal/telemetry/traces/field_mapping.go`): `enrichAttribute(raw)` maps a **prefixed** raw name to a `TraceAttribute{name, semantic_name, type, filter_field, hint}`:
- `resource_service.name` / `service.name` → `ServiceName` (toplevel)
- `resource_*` → `resources['<stripped>']`
- `event_*` → `events['<stripped>']`
- known toplevel fields (`TraceId`, `SpanName`, `StatusCode`, …) → used as-is
- `grpc.status_code` → `attributes['rpc.grpc.status_code']`
- everything else → `attributes['<raw>']`

**Response shape mismatch:** the series endpoint returns `{data: [Record<string,string>]}` whose keys are prefixed (`resource_department`, `event_exception.type`, bare span attrs). The global `/search/tags` endpoint instead returns scopes with prefixes already stripped:

```json
{ "scopes": [
  { "name": "span",     "tags": ["http.method", ...] },
  { "name": "resource", "tags": ["department", ...] },
  { "name": "event",    "tags": ["exception.type", ...] }
]}
```

So the global fix must **re-prefix** each scope tag before calling `enrichAttribute`, so existing enrichment logic is reused unchanged.

**Cache note:** `internal/attributes/cache.go` fetches `traceAttrs` via `FetchTraceAttributeNames` but **no code reads it** — there is no `GetTraceAttributes()` getter, and only `GetLogAttributes()` feeds the `get_logs` `{{labels}}` placeholder. Changing the global trace endpoint therefore affects no prompt injection.

## Design

All changes in `internal/telemetry/traces/` plus tool registration in `tools.go` and one constant in `internal/constants/api.go`.

### Change 1 — New tool `get_trace_attributes_for_pipeline`

New file `internal/telemetry/traces/series_attributes.go`, mirroring `internal/telemetry/logs/series_attributes.go`.

**Args:**
```go
type GetTraceAttributesForPipelineArgs struct {
    Pipeline        []map[string]interface{} `json:"pipeline,omitempty" jsonschema:"Pipeline of prior filter stages to scope discovery, e.g. [{\"type\":\"filter\",\"query\":{\"$eq\":[\"ServiceName\",\"<service>\"]}}] (required)"`
    LookbackMinutes int                      `json:"lookback_minutes,omitempty" jsonschema:"Number of minutes to look back from now (default: 15, minimum: 1)"`
    StartTimeISO    string                   `json:"start_time_iso,omitempty" jsonschema:"Start time in RFC3339/ISO8601 format"`
    EndTimeISO      string                   `json:"end_time_iso,omitempty" jsonschema:"End time in RFC3339/ISO8601 format"`
    Region          string                   `json:"region,omitempty" jsonschema:"Region to query (optional). Defaults to configured region."`
}
```

**Behavior:**
- `pipeline` is **required**; empty → error mirroring the logs tool ("pipeline parameter is required. Provide at least one filter stage…").
- Time window via `utils.GetTimeRange(params, 15)`, defaulting to last 15 minutes.
- `POST` the user's pipeline to `EndpointTracesSeries` (`/cat/api/traces/v2/series/json`) with `region/start/end` query params and standard headers (`constants.Header*`, bearer token).
- Decode `TraceAttributesResponse` (`{data: [map[string]string], status}`); union the keys of `data[0]` (consistent with existing `get_trace_attributes`), skip empty keys, sort.
- Enrich each raw name via `enrichAttribute`; marshal the `[]TraceAttribute` array as JSON text.

**Refactor to share the series fetch:** extract the series-POST-and-collect-names logic currently inlined in `attributes.go`'s handler into a helper `fetchTraceSeriesAttributeNames(ctx, client, cfg, pipeline, start, end, region) ([]string, error)`. Both the new pipeline handler and `FetchTraceAttributeNames` reuse it (the pipeline handler passes the user pipeline; the legacy fetch passes the empty pipeline). This avoids duplicating the HTTP plumbing.

**Description constant** `GetTraceAttributesForPipelineDescription`: adapt the logs pipeline description to traces — emphasize "scoped to your in-progress pipeline", "use `filter_field` directly", and the workflow (filter stage → call this → build `get_traces` filter). Drop the log-index rules (not applicable to traces).

### Change 2 — Fix global `get_trace_attributes`

Switch the existing handler from empty-pipeline series to `GET /cat/api/search/tags`.

- Add constant `EndpointTraceTags = "/cat/api/search/tags"` in `internal/constants/api.go`.
- New response type:
  ```go
  type traceTagsScopesResponse struct {
      Scopes []struct {
          Name string   `json:"name"` // "span" | "resource" | "event"
          Tags []string `json:"tags"`
      } `json:"scopes"`
  }
  ```
- `GET` with `region/start/end` query params and standard headers (no body).
- **Re-prefix** each scope tag back to the raw form `enrichAttribute` expects, then enrich:
  - `name == "resource"` → `resource_<tag>`
  - `name == "event"` → `event_<tag>`
  - `name == "span"` (or any other) → `<tag>` (bare; `enrichAttribute` handles toplevel/grpc/default)
- Sort the resulting raw names, enrich, marshal as the same `[]TraceAttribute` JSON. **Output shape is unchanged**, so no consumer breaks.
- Keep the existing `GetTraceAttributesArgs` (time window + region) unchanged.

Update `FetchTraceAttributeNames` to use the same `/search/tags` source for consistency (optional but recommended; currently unread — see cache note). If updated, it returns the re-prefixed raw names so any future consumer matches the tool output.

### Change 3 — Pipeline-aware `get_trace_attribute_values`

In `attribute_values.go`:
- Add optional `Pipeline []map[string]interface{}` to `GetTraceAttributeValuesArgs` (jsonschema: optional, "Pipeline of prior filter stages to scope values to a slice; omit for global values").
- When `len(args.Pipeline) > 0`, send it as the request body's `pipeline`; otherwise keep the current hardcoded empty filter. Endpoint, normalization (`normalizeTagName`), enrichment, and output shape are unchanged. Fully backward compatible.

### Tool registration (`tools.go`)

Register `get_trace_attributes_for_pipeline` immediately after `get_trace_attributes` (parallel to how `get_log_attributes_for_pipeline` follows `get_log_attributes`), with a short comment noting it discovers fields present for a given pipeline via the series endpoint.

## Error handling

Mirror existing handlers: non-2xx responses decode an error body and return `fmt.Errorf("API returned status %d: %v", …)`; non-`success` status strings return an error; JSON decode failures are wrapped. The pipeline tool additionally rejects an empty `pipeline` argument up front.

## Testing

Add tests mirroring the logs test files, using the existing `httptest`-based patterns in the traces package:

- `series_attributes_test.go` — pipeline tool: (a) missing pipeline → error; (b) series response with mixed prefixes → correct enriched `filter_field`s; (c) time-window param plumbing; (d) non-success status → error.
- `attributes_test.go` (extend) — global tool against a `/search/tags` scopes payload: resource/event/span re-prefixing produces the right `filter_field`s; `service.name` special-case → `ServiceName`; empty scopes → "no attributes" message.
- `attribute_values_test.go` (extend) — with-pipeline vs without-pipeline both produce correct request bodies and identical output shape.

Run `go build ./...` and `go test ./internal/telemetry/traces/...` before opening the PR.

## Files touched

- `internal/telemetry/traces/series_attributes.go` (new)
- `internal/telemetry/traces/series_attributes_test.go` (new)
- `internal/telemetry/traces/attributes.go` (global endpoint swap + shared series helper)
- `internal/telemetry/traces/attribute_values.go` (optional pipeline)
- `internal/telemetry/traces/attributes_test.go`, `attribute_values_test.go` (extend)
- `internal/constants/api.go` (`EndpointTraceTags`)
- `tools.go` (register new tool)
- `internal/attributes/cache.go` — only if `FetchTraceAttributeNames` is repointed (optional)

## Considered & rejected

- **Single tool with an optional pipeline arg** — fewer tools, but breaks logs/trace symmetry and muddies the LLM's tool choice. The two-tool split is the established, working pattern.
- **Add only the pipeline tool, leave global on empty-pipeline series** — smallest diff, but leaves the latent undercount bug in `get_trace_attributes`.
