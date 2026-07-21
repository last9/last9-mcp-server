# last9-mcp-server — agent instructions

Canonical agent guidance for this repo. `CLAUDE.md` is a compatibility shim that includes this file — edit here, not there.

## Adding or changing an MCP tool

### Tool descriptions: one style, no exceptions

All tool description text lives as markdown in `internal/prompts/descriptions/`, embedded via `go:embed` in `internal/prompts/prompts.go`. Never define description text as Go string constants in handler packages.

For a new tool `get_foo`:

1. Write `internal/prompts/descriptions/get_foo.md` — the complete tool description.
2. Add to `internal/prompts/prompts.go`:
   ```go
   //go:embed descriptions/get_foo.md
   var GetFooDescription string
   ```
3. Register in `tools.go` with `last9mcp.RegisterInstrumentedTool(server, &mcp.Tool{Name: "get_foo", Description: prompts.GetFooDescription}, foo.NewGetFooHandler(client, cfg))`.

Tools whose descriptions are enhanced at runtime (`buildEnhancedDescription`: base + appended instructions + `{{labels}}` substitution) use two files: `<tool>_base.md` (base) and `<tool>.md` (appended instructions). Only do this when the description needs runtime substitution; otherwise one file. (Grandfathered asymmetries: `prometheus_range_query_base.md` pairs with `get_metrics.md`; `get_exceptions` uses an `Instructions`-suffixed var as its plain description.)

Some description files intentionally end without a trailing newline — editors or formatters that auto-append one silently change the served description and break `dump-tools` snapshot diffs. Preserve file bytes exactly when editing.

Why markdown-only: Go constants are invisible to the eval harness and docs tooling, and a parallel `.md` copy drifts (a stale `get_alerts.md` once taught models a `window` param shape the server rejected). `go:embed` makes the file the single source; a bad path fails the build.

### Argument structs

- Define each tool's `Args` struct in the tool's own handler file, alongside its `New<Tool>Handler` (e.g. `GetFooArgs` in `foo/get_foo.go`). This is the repo-wide convention across every package (`alerting`, `apm`, `telemetry/logs`, `telemetry/traces`, and `dashboards`' own `get.go`/`list.go`).
- JSON tags: `snake_case`, `omitempty` on optionals. `jsonschema:` tag carries the param description; prefix required params' description with `(Required)`.
- The SDK infers `additionalProperties: false` from the struct; unknown keys are rejected before the handler runs.

### Verifying description/schema changes

- `go run . dump-tools` prints the served tools/list (`{"tools": [...]}`, name-sorted) with no credentials — the canonical snapshot for evals and docs.
- Eval harness: the last9-mcp-evals repo. Run suites against this checkout with `--tools-json=$(pwd)/tools.json` after `go build -o last9-mcp . && ./last9-mcp dump-tools > tools.json` (flag lands in last9-mcp-evals#12; until merged use `--use-server`).

### Description content rules

- Document every parameter, defaults, and units. Unit mistakes propagate straight into model behavior (a doc example using milliseconds for the nanosecond `Duration` field produced wrong queries in production — every example must use correct units).
- Avoid attribute-name allowlists models could over-anchor on; point to discovery tools instead.
- When two params overlap (e.g. a seconds window and a minutes lookback), say explicitly which one to prefer and the valid range of each.
