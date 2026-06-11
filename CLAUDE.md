# last9-mcp-server — agent instructions

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
3. Register in `tools.go` with `registerTool(server, reg, &mcp.Tool{Name: "get_foo", Description: prompts.GetFooDescription}, foo.NewGetFooHandler(client, cfg))`. The `registerTool` wrapper records parameter names for the paramhint middleware — do not call `RegisterInstrumentedTool` directly.

Tools whose descriptions are enhanced at runtime (`buildEnhancedDescription`: base + appended instructions + `{{labels}}` substitution) use two files: `<tool>_base.md` (base) and `<tool>.md` (appended instructions). Only do this when the description needs runtime substitution; otherwise one file.

Why markdown-only: Go constants are invisible to the eval harness and docs tooling, and a parallel `.md` copy drifts (a stale `get_alerts.md` once taught models a `window` param shape the server rejected). `go:embed` makes the file the single source; a bad path fails the build.

### Argument structs

- JSON tags: `snake_case`, `omitempty` on optionals. `jsonschema:` tag carries the param description; prefix required params' description with `(Required)`.
- Naming: service filters use `service_name` (canonical). When an ecosystem prior or sibling-tool inconsistency makes another name likely (e.g. Prometheus's `match`), add an alias field and coalesce in the handler — canonical wins when both set. See `PromqlLabelValuesArgs.Match` and `ServiceSummaryArgs.Service`.
- The SDK infers `additionalProperties: false` from the struct; unknown keys are rejected before the handler runs. The paramhint middleware (`internal/paramhint`) appends valid-param lists and one did-you-mean suggestion to those errors — keep it wired via `registerTool`.

### Verifying description/schema changes

- `go run . dump-tools` prints the served tools/list (`{"tools": [...]}`, name-sorted) with no credentials — the canonical snapshot for evals and docs.
- Session-level tests in `schema_validation_test.go` run the server over in-memory transports and exercise real SDK validation (direct handler calls bypass it). Add a case there when changing schemas or aliases.
- Eval harness lives at `~/Projects/last9-mcp-evals`; run suites against this checkout with `--tools-json=$(pwd)/tools.json` after `go build -o last9-mcp . && ./last9-mcp dump-tools > tools.json`.

### Description content rules

- Document every parameter, defaults, and units. Unit mistakes propagate straight into model behavior (a doc example using milliseconds for the nanosecond `Duration` field produced wrong queries in production — every example must use correct units).
- Avoid attribute-name allowlists models could over-anchor on; point to discovery tools instead.
- When two params overlap (e.g. a seconds window and a minutes lookback), say explicitly which one to prefer and the valid range of each.
