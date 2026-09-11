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
3. Register in `tools.go` with `reg(registerIfAllowed(server, cfg.AllowedTools, &mcp.Tool{Name: "get_foo", Description: prompts.GetFooDescription}, foo.NewGetFooHandler(client, cfg)))`, and add `get_foo` to its domain in `internal/toolsets/toolsets.go`.

   Never call `last9mcp.RegisterInstrumentedTool` directly from `tools.go`. `registerIfAllowed` is what enforces `--toolsets` filtering, surfaces registration errors instead of discarding them, and recovers the SDK's panic on an invalid tool schema. A tool registered directly leaks into every toolset — verify with `go run . dump-tools --toolsets=<other-domain>` that the new tool is absent.

**Progressive disclosure (whales):** `get_logs`, `get_traces`, `get_service_logs`, and `prometheus_range_query` serve a short description (`*_base.md`) with firing blurb + critical rules + a `last9://reference/...` pointer. Full manuals live in `internal/prompts/references/` (`logjson.md`, `tracejson.md`, `service_logs.md`, `metrics.md`), embedded and registered as MCP resources in `resources.go`. Do not concatenate long manuals back into `tools/list`. Do not inject org attribute catalogs into descriptions — point at discovery tools.

The `*_base.md` suffix means progressive disclosure and nothing else — a description with no manual of its own must not carry it. Grandfathered: `get_exceptions` uses an `Instructions`-suffixed var as its plain description. Prefer a single description file for new tools unless progressive disclosure is required.

Some description files intentionally end without a trailing newline — editors or formatters that auto-append one silently change the served description and break `dump-tools` snapshot diffs. Preserve file bytes exactly when editing.

Why markdown-only: Go constants are invisible to the eval harness and docs tooling, and a parallel `.md` copy drifts (a stale `get_alerts.md` once taught models a `window` param shape the server rejected). `go:embed` makes the file the single source; a bad path fails the build.

### Toolsets

- CLI/env: `--toolsets` / `LAST9_TOOLSETS` (alias `LAST9_MCP_TOOLSETS`). Comma-separated: `logs`, `traces`, `metrics`, `alerts`, `dashboards`, `investigate`, `all`.
- Empty / unset / `all` → full surface. Unknown names fail fast with the valid list. Membership lives in `internal/toolsets`.
- `dump-tools` honors the same flags/env (loads `.env` first). Canonical snapshot is unset/`all`.

### Argument structs

- JSON tags: `snake_case`, `omitempty` on optionals. `jsonschema:` tag carries the param description; prefix required params' description with `(Required)`.
- The SDK infers `additionalProperties: false` from the struct; unknown keys are rejected before the handler runs.

### Verifying description/schema changes

- `go run . dump-tools` prints the served tools/list (`{"tools": [...]}`, name-sorted) with no credentials — the canonical snapshot for evals and docs. Use `--toolsets=investigate` to measure the automation surface.
- Eval harness: the last9-mcp-evals repo. Point it at this checkout with `LAST9_MCP_SERVER_PATH=$(pwd)` and prefer `--use-server` so suites see served short descriptions + resources rather than stale markdown paths. Example:
  ```bash
  ./scripts/eval-r10.sh
  # or manually:
  go build -o bin/last9-mcp .
  cd ../last9-mcp-evals
  LAST9_MCP_SERVER_PATH=../last9-mcp-server npm run eval:log -- --use-server
  ```
  (`--tools-json` lands in last9-mcp-evals#12 when available.)

### Description content rules

- Document every parameter, defaults, and units. Unit mistakes propagate straight into model behavior (a doc example using milliseconds for the nanosecond `Duration` field produced wrong queries in production — every example must use correct units).
- Avoid attribute-name allowlists models could over-anchor on; point to discovery tools instead.
- When two params overlap (e.g. a seconds window and a minutes lookback), say explicitly which one to prefer and the valid range of each.
- Critical query-construction rules for whales must remain on the tool description even when the long manual is a resource.
- Write-pair tools (`create_*` / `update_*`) must state **net-new** vs **refine** in the description: create once, keep the returned id, refine with update. Do not require list-before-create unless product asks. Put this copy in the description markdown, not in Go schema strings.

## Adding a tool, end to end

1. **New API endpoint?** Add `internal/last9api/<area>/<endpoint>.go` — see *Calling the
   Last9 API*. Skip if the endpoint already exists.
2. **Description** — write `internal/prompts/descriptions/<tool_name>.md`, embed it in
   `internal/prompts/prompts.go` as `<Tool>Description`.
3. **Tool** — create `<package>/<tool_name>.go` with its `Args` struct, its
   `New<Tool>Handler`, and its private helpers — see *File layout: one tool per file*.
4. **Register** — add to `tools.go` via `registerIfAllowed`, and add the tool name to its
   domain in `internal/toolsets/toolsets.go`.
5. **Test** — `<tool_name>_test.go` using `httptest.NewServer`.
6. **Verify** — `go run . dump-tools` shows the tool with the intended description and schema.
7. **CHANGELOG** — one bullet at the top of its section in `[Unreleased]`.

Example — `get_databases`:
`internal/last9api/databases/database_query.go` → `descriptions/get_databases.md` →
`internal/apm/get_databases.go` → registered in `tools.go`, toolset `metrics`.

## Calling the Last9 API

All HTTP calls to the Last9 API go through `internal/last9api`. Tool packages do not
build URLs or set auth headers.

```
internal/last9api/
    client.go              transport: base URL, auth, region, JSON, errors
    constants.go           headers and other transport-level constants
    types.go               types shared across areas
    <area>/<endpoint>.go   one file per endpoint
    <area>/types.go        types shared by two endpoints in the same area
```

**Area** is the product area the endpoint belongs to (`databases`, `logs`, `traces`, ...).
Create the folder when you add its first endpoint.

**File** is named after the endpoint's distinctive path segments, `-` to `_`, dropping the
area name, version prefixes, and path params:

| endpoint | file |
|---|---|
| `/database-query` | `databases/database_query.go` |
| `/cat/api/traces/v2/query_range/json` | `traces/query_range.go` |
| `/entities/{entity_id}/kpis/{kpi_id}` | `entities/kpis.go` |

An endpoint file holds its path const, its input and response types, and its functions.

### Writing an endpoint

```go
const pathDatabaseQuery = "/database-query"

const opDatabaseQuery = "database query"

// DiscoverDatabases calls POST /database-query with template=discover.
func DiscoverDatabases(
    ctx context.Context, c *last9api.Client, in DiscoverDatabasesInput,
) (*DiscoverDatabasesResponse, []FieldError, error) {
    var out queryResult
    err := c.Do(ctx, opDatabaseQuery,
        last9api.Request{Method: http.MethodPost, Path: pathDatabaseQuery, Body: body},
        &out,
        last9api.WithRegionHeader(),
    )
    ...
}
```

```go
func (c *Client) Do(ctx context.Context, op string, r Request, out any, opts ...Option) error
type Request struct{ Method, Path string; Body any }
```

- `op` is a short lowercase phrase naming the call (`"database query"`). It appears in
  error messages, so make it readable.
- `out` may be `nil` when the response body is not needed.
- `Do` maps every non-2xx to a sanitized error. Endpoint code never inspects status codes.
- Options — `WithQuery`, `WithRegionHeader`, `WithRegionQuery` — are passed only by
  endpoints that need them. `WithQuery` merges with the region parameter.
- A response the API reports as partial is returned alongside its field errors rather than
  as a failure; the tool surfaces them.

### Rules

**1. `client.go` stays generic.** It must never name an endpoint. If something does not
fit, add an option.
```go
OK:  c.Do(ctx, "logs query", req, &out, last9api.WithRegionQuery())
NO:  if r.Path == "/database-query" { ... }   // inside client.go
```

**2. Copy the path exactly as the API defines it.** Never add or strip a trailing slash —
some collection routes require one and the API does not redirect.
```go
OK:  Path: "/logs/searches/"
NO:  strings.TrimSuffix(path, "/")
```

**3. Region has three placements, and the endpoint declares which.**
```go
OK:  last9api.WithRegionHeader()    OK:  last9api.WithRegionQuery()
OK:  body.Region = c.Region()       // endpoints that take region in the body
NO:  httpReq.Header.Set("region", ...)
```

**4. Imports go one way:** `last9api` to `utils`/`models`/`constants`, never back. A
`utils` API helper *moves into* `last9api` when migrated; wrapping it will not compile.

**5. Areas import the parent, never each other.** A type two areas need moves to
`last9api/types.go`; a type two endpoints in one area need goes in that area's `types.go`.

**6. New endpoints keep their path const in the endpoint file**, not in
`internal/constants`. Paths already in `internal/constants` stay there until their caller
migrates.

**7. Test with `httptest.NewServer`.** The `*http.Client` is the seam; do not add an
interface for it.

### Naming

- Functions are `<Verb><Resource>`, fully spelled even when it repeats the package:
  `dashboards.GetDashboard`, `databases.DiscoverDatabases`. Never a bare `Get` or `List`.
- Prefer `Get` (one), `List` (many), `Create`/`Update`/`Delete`, `Query` (time-range
  search). A domain verb is fine when it names the operation better (`Discover`).
- A function in an endpoint file that issues a call starts its doc comment with the method
  and path: `// GetDashboard calls GET /dashboards/{id}.` Nothing else needs a doc comment
  — not `client.go`, not helpers like `EnvFilter`.
- Types are `<Op>Input` and `<Op>Response`. An endpoint returning a bare JSON array may
  return a slice instead.
- An area package whose name collides with a tool package is imported with an `api` suffix:
  `apilogs "last9-mcp/internal/last9api/logs"`.

**Exception:** the OAuth access-token call stays in `internal/auth`. It uses a different
host, sends no auth header, and mints the token `last9api` depends on.

## File layout: one tool per file

A tool goes in `<package>/<tool_name>.go` with its `Args` struct, its `New<Tool>Handler`,
and its private helpers; tests in `<tool_name>_test.go`. Helpers shared by two or more
tools go in a file named for the concern (`http.go`, `validate.go`, `result.go`), not in
the package's namesake file.

The `Args` struct lives in that same file, next to the handler it serves — never in a
shared `models.go` or in the package's namesake file.

Handlers keep the existing `New<Tool>Handler(client *http.Client, cfg models.Config)`
signature and build their own client inside:

```go
resp, err := databases.DiscoverDatabases(ctx, last9api.NewClient(client, cfg), in)
```

Most packages already follow the one-tool-per-file layout. `apm/apm.go`,
`apm/databases.go`, `alerting/alerts.go` and `telemetry/logs/drop_rule.go` predate it and
hold several tools each. Split a tool out when you change that tool — not as a sweep, and
not for an unrelated edit to the same file. New tools get no exemption.

## Code comments

Don't add explanatory comments. Code should read on its own.

The only comment worth writing records non-obvious **why**: a constraint, a gotcha, why a
guard must stay. Keep it to 3 lines. If the why needs more than that, fix the code instead
— better name, smaller function, extracted helper.

```go
OK:  // Uppercase is folded rather than rejected: the trace-detail API 400s on uppercase hex.
NO:  // loop over the rows and build the summary
```

Exception: a function in an `internal/last9api/<area>` endpoint file that issues a call
starts its doc comment with the method and path
(`// GetDashboard calls GET /dashboards/{id}.`).

Leave existing comments alone.

## Functions

One responsibility per function. Use clear, self-explanatory names for functions,
parameters, and variables.

Take at most 3 parameters, not counting `ctx` and variadic options. More than 3 means a
struct, unless a struct genuinely makes it less readable.

## CHANGELOG

`CHANGELOG.md` follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/). Add your entry to `[Unreleased]` in the same PR as the change — a release cut only renames the heading, it does not go hunting for missing entries.

### What earns an entry

Write one when a user of the server would otherwise hold a wrong belief: the tool returns different data, a filter or parameter now behaves differently, `isError` or a response shape changes, the process stops crashing, or a query is no longer injectable. "It was an obscure input" is not a reason to skip — `min_duration_ms` silently ignored on half the results is the same defect class as a wrong number, and both belong here.

Skip dead-code removals, internal refactors, and test-only changes. Skip hardening for inputs no caller can produce (a malformed pipeline the validator already rejects, arithmetic that needs timestamps near ±2^63). Dependency bumps get a single line under `Changed`, collapsed across PRs: `Bumped 'x/y' 1.2.3 → 1.2.5 (#231, #272)`.

### Format

- Sections are `### Added`, `### Fixed`, `### Changed`, in that order. One contiguous run of bullets per section — no blank lines between them.
- Newest first: a new bullet goes at the **top** of its section. After merging main into your branch, put your entry above the one you merged in.
- End every bullet with the **PR** number, not the issue it closes: `… (#274).` A reader needs the diff, and the issue is one click from the PR anyway.
- State the observable behavior and the mechanism behind it. These entries are read by people debugging a version difference, so "fixed a bug in get_foo" is useless — say what was wrong, what they saw, and what they see now.

### Cutting a release

Rename `## [Unreleased]` to `## [X.Y.Z] - YYYY-MM-DD`, add a fresh empty `[Unreleased]` above it, and bump `version` in `package.json` — that bump is what the release workflow detects. Minor when tools or resources are added, patch for fixes alone; mark any breaking change with a leading `**Breaking:**`.
