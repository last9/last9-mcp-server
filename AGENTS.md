# last9-mcp-server — agent instructions

Canonical agent guidance for this repo. `CLAUDE.md` is a compatibility shim that includes
this file — edit here, not there.

This is a **public** repo. Never put internal backend paths, handler or route names,
customer names, or infrastructure identifiers in code, comments, docs, commits, or PRs.

## Orientation

An MCP server in Go (module `last9-mcp`) that exposes Last9 observability as tools,
prompts, and resources.

**Root files**

| file | role |
|---|---|
| `main.go` | startup, config, subcommands, stdio/HTTP transport |
| `tools.go` | every tool registration |
| `resources.go` | reference resources (`last9://reference/...`) |
| `dump.go` | `dump-tools` / `dump-prompts` — builds a server without credentials |
| `http_server.go` | `--http` mode |

**`internal/`**

| package | role |
|---|---|
| `last9api` | the single door for HTTP calls to the Last9 API |
| `apm`, `alerting`, `dashboards`, `telemetry`, `change_events`, `suggest` | tool handlers |
| `prompts` | embedded tool descriptions and reference manuals |
| `workflows` | MCP prompts |
| `toolsets` | which tools belong to which `--toolsets` domain |
| `models`, `constants`, `utils`, `auth`, `deeplink`, `otelids` | shared plumbing |

**Commands**

```bash
go build .
go test -race ./...          # integration tests skip without TEST_REFRESH_TOKEN
go run . dump-tools          # served tools/list, name-sorted, no credentials
go run . dump-prompts        # served prompts/list
./scripts/start-local.sh     # run against a local .env
./scripts/eval-r10.sh        # eval suites against a fresh build of this checkout
```

## Adding a tool, end to end

1. **New API endpoint?** Add `internal/last9api/<area>/<endpoint>.go` — see *Calling the
   Last9 API*. Skip if the endpoint already exists.
2. **Description** — write `internal/prompts/descriptions/<tool_name>.md` and embed it in
   `internal/prompts/prompts.go` as `<Tool>Description` — see *Tool descriptions*.
3. **Tool** — create `<package>/<tool_name>.go` with its `Args` struct, its
   `New<Tool>Handler`, and its private helpers — see *Go conventions*.
4. **Register** — add to `tools.go` via `registerIfAllowed`, and add the tool name to its
   domain in `internal/toolsets/toolsets.go` — see *Registration*.
5. **Test** — `<tool_name>_test.go` using `httptest.NewServer`; run `go test -race ./...`.
6. **Verify** — `go run . dump-tools` shows the tool with the intended description and
   schema.
7. **CHANGELOG** — one bullet at the top of its section in `[Unreleased]`.

Example — `get_databases`: `internal/last9api/databases/database_query.go` →
`descriptions/get_databases.md` → `internal/apm/get_databases.go` → registered in
`tools.go`, toolset `metrics`.

## Tool descriptions

**Description text lives in markdown, never in Go string constants.** Go constants are
invisible to the eval harness and docs tooling, and a parallel `.md` copy drifts. `go:embed`
makes the file the single source; a bad path fails the build.

```go
OK:  //go:embed descriptions/get_foo.md
     var GetFooDescription string
NO:  const getFooDescription = "Fetches..."   // in a handler package
```

**Name the var `<Tool>Description`.** Legacy vars use `…Details` (the `prometheus_*` tools,
`get_service_performance_details`, `get_service_dependency_graph`) or `…Instructions`
(`get_exceptions`). Leave them alone; do not copy the pattern.

**Preserve file bytes exactly.** Several description files intentionally end without a
trailing newline. No test enforces this — an editor or formatter that auto-appends one
silently changes the served description and breaks `dump-tools` snapshot diffs.

### Progressive disclosure

The four highest-token tools — `get_logs`, `get_traces`, `get_service_logs`,
`prometheus_range_query` — serve a short `*_base.md` description (firing blurb + critical
rules + a `last9://reference/...` pointer). Their full manuals live in
`internal/prompts/references/` (`logjson.md`, `tracejson.md`, `service_logs.md`,
`metrics.md`, `investigation.md`) and are registered as MCP resources in `resources.go`.

- The `*_base.md` suffix means progressive disclosure and nothing else. A description with
  no manual of its own must not carry it.
- Never concatenate a long manual back into `tools/list`.
- Prefer a single description file for a new tool unless progressive disclosure is required.

### Content rules

**Document every parameter, its default, and its unit.** Unit mistakes propagate straight
into model behavior — a doc example using milliseconds for a nanosecond field produced
wrong queries in production.

**Point at discovery tools, not attribute allowlists.** Models over-anchor on a list and
stop discovering. Never inject an org's attribute catalog into a description.

**When two params overlap, say which to prefer.** e.g. a seconds window against a minutes
lookback — state the preference and the valid range of each.

**Keep critical query-construction rules on the description** even when the long manual is
a resource.

**Write-pair tools (`create_*` / `update_*`) must state net-new vs refine:** create once,
keep the returned id, refine with update. Put this copy in the description markdown, not in
a Go schema string. Do not require list-before-create unless product asks.

## Registration

### Tools

**Register through `registerIfAllowed`, never `last9mcp.RegisterInstrumentedTool`.**
`registerIfAllowed` is what enforces `--toolsets` filtering, surfaces registration errors
instead of discarding them, and recovers the SDK's panic on an invalid tool schema. A tool
registered directly leaks into every toolset.

```go
reg(registerIfAllowed(server, cfg.AllowedTools,
    &mcp.Tool{Name: "get_foo", Description: prompts.GetFooDescription},
    foo.NewGetFooHandler(client, cfg)))
```

Then add `get_foo` to its domain in `internal/toolsets/toolsets.go`, and verify with
`go run . dump-tools --toolsets=<other-domain>` that it is absent there. `dump_test.go`
asserts per-toolset membership and copy — update it in the same change.

### Prompts and resources

**`workflows.Register` and `registerReferenceResources` are siblings of `registerAllTools`,
at all three call sites.** They are not tools and must not be nested inside
`registerAllTools`.

Three call sites: `main.go`, and `dump.go` twice — once for `dump-tools`, once for
`dump-prompts`. Miss one and `dump_prompts_test.go` fails on a prompt count with no hint why.

MCP prompts live one-per-file in `internal/workflows/`, with their text embedded in
`internal/prompts/prompts.go` as `<Name>Workflow`.

### Argument structs

**JSON tags are `snake_case`, with `omitempty` on optionals.** The `jsonschema:` tag
carries the param description; prefix a required param's description with `(Required)`.

```go
StartTimeISO string `json:"start_time_iso,omitempty" jsonschema:"Start of the window, RFC3339"`
Service      string `json:"service" jsonschema:"(Required) Service name"`
```

The SDK infers `additionalProperties: false` from the struct, so unknown keys are rejected
before the handler runs. Seven tools instead pass a hand-built `InputSchema:` (`get_logs`,
`get_traces`, `get_trace_waterfall`, `get_service_summary`, `get_apm_service_deviations`,
`create_dashboard`, `update_dashboard`) — those do **not** get the inferred strictness. Use
a struct for new tools; reach for `InputSchema:` only when the schema cannot be expressed as
one.

### Toolsets

- CLI/env: `--toolsets` / `LAST9_TOOLSETS` (alias `LAST9_MCP_TOOLSETS`). Comma-separated:
  `logs`, `traces`, `metrics`, `alerts`, `dashboards`, `investigate`, `all`.
- Empty / unset / `all` → full surface. Unknown names fail fast with the valid list.
- `dump-tools` honors the same flags and env, and loads `.env` first. The canonical snapshot
  is unset/`all`; use `--toolsets=investigate` to measure the automation surface.

## Calling the Last9 API

All HTTP calls to the Last9 API go through `internal/last9api`. Tool packages do not build
URLs or set auth headers.

```
internal/last9api/
    client.go              transport: base URL, auth, region, JSON, errors
    constants.go           headers and other transport-level constants
    types.go               types shared across areas          (add when first needed)
    <area>/<endpoint>.go   one file per endpoint
    <area>/types.go        types shared by two endpoints in one area
```

Only `databases/` has migrated so far; the rest of the repo still calls the API directly.
Move a call site here when you touch it — not as a sweep.

**Area** is the product area the endpoint belongs to (`databases`, `logs`, `traces`, …).
Create the folder when you add its first endpoint.

**File** is named after the endpoint's distinctive path segments, `-` to `_`, dropping the
area name, version prefixes, and path params:

| endpoint | file |
|---|---|
| `/database-query` | `databases/database_query.go` |
| `/traces/v2/query_range/json` | `traces/query_range.go` |
| `/entities/{entity_id}/kpis/{kpi_id}` | `entities/kpis.go` |

An endpoint file holds its path const, its input and response types, and its functions.

### The client

```go
func (c *Client) Do(ctx context.Context, op string, r Request, out any, opts ...Option) error
type Request struct{ Method, Path string; Body any }
```

- `op` is a short lowercase phrase naming the call (`"database query"`). It appears in error
  messages, so make it readable.
- `out` may be `nil` when the response body is not needed.
- `Do` maps every non-2xx to a sanitized error — endpoint code never inspects status codes.
- Options (`WithQuery`, `WithRegionHeader`, `WithRegionQuery`) are passed only by endpoints
  that need them. `WithQuery` merges with the region parameter.
- A response the API reports as partial comes back alongside its field errors rather than as
  a failure; the tool surfaces them.

### Rules

**1. `client.go` stays generic.** It must never name an endpoint. If something does not fit,
add an option.
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

**4. Imports go one way:** `last9api` → `utils` / `models` / `constants`, never back. A
`utils` API helper *moves into* `last9api` when migrated.
```go
OK:  // delete the utils helper, its callers now call last9api
NO:  func Do(...) { return utils.CallLast9API(...) }
```

**5. Areas import the parent, never each other.** A type two areas need moves to
`last9api/types.go`; a type two endpoints in one area need goes in that area's `types.go`.
```go
OK:  databases → last9api
NO:  databases → last9api/traces
```

**6. A new endpoint keeps its path const in its own file**, not in `internal/constants`.
Paths already in `internal/constants` stay there until their caller migrates.
```go
OK:  const pathDatabaseQuery = "/database-query"   // in databases/database_query.go
NO:  constants.EndpointDatabaseQuery
```

**7. Test with `httptest.NewServer`.** The `*http.Client` is the seam; do not add an
interface for it.
```go
OK:  srv := httptest.NewServer(handler); NewClient(srv.Client(), cfg)
NO:  type Doer interface{ Do(*http.Request) (*http.Response, error) }
```

**8. Exception: the OAuth access-token call stays in `internal/auth`.** It uses a different
host, sends no auth header, and mints the token `last9api` depends on.

### Naming

- Functions are `<Verb><Resource>`, fully spelled even when it repeats the package:
  `dashboards.GetDashboard`, `databases.DiscoverDatabases`. Never a bare `Get` or `List`.
- Prefer `Get` (one), `List` (many), `Create` / `Update` / `Delete`, `Query` (time-range
  search). A domain verb is fine when it names the operation better (`Discover`).
- Types are `<Op>Input` and `<Op>Response`. An endpoint returning a bare JSON array may
  return a slice instead.
- An area package whose name collides with a tool package is imported with an `api` suffix:
  `apilogs "last9-mcp/internal/last9api/logs"`.

## Go conventions

### One tool per file

A tool goes in `<package>/<tool_name>.go` with its `Args` struct, its `New<Tool>Handler`,
and its private helpers; tests in `<tool_name>_test.go`. The `Args` struct lives next to the
handler it serves — never in a shared `models.go` or in the package's namesake file.
Helpers shared by two or more tools go in a file named for the concern (`http.go`,
`validate.go`, `result.go`).

Handlers keep the `New<Tool>Handler(client *http.Client, cfg models.Config)` signature and
build their API client inside:

```go
resp, err := databases.DiscoverDatabases(ctx, last9api.NewClient(client, cfg), in)
```

Most packages already follow this. `apm/apm.go`, `apm/databases.go`, `alerting/alerts.go`
and `telemetry/logs/drop_rule.go` predate it and hold several tools each. Split a tool out
when you change that tool — not as a sweep, and not for an unrelated edit to the same file.
New tools get no exemption.

### Comments

**Don't add explanatory comments.** Code should read on its own. The only comment worth
writing records a non-obvious **why**: a constraint, a gotcha, why a guard must stay. Keep
it to 3 lines — if the why needs more, fix the code instead (better name, smaller function,
extracted helper).

```go
OK:  // Uppercase is folded rather than rejected: the trace-detail API 400s on uppercase hex.
NO:  // loop over the rows and build the summary
```

Leave existing comments alone.

Exception: a function in an `internal/last9api/<area>` endpoint file that issues a call
starts its doc comment with the method and path.

```go
// GetDashboard calls GET /dashboards/{id}.
```

### Functions

**Take at most 3 parameters**, not counting `ctx` and variadic options. More than 3 means a
struct, unless a struct genuinely makes it less readable.

### Contracts

`contracts/` holds JSON schemas and fixtures pinned by sha256 in `contracts_test.go`.
Editing one without updating its pin fails a test whose message will not explain itself.

## CHANGELOG

`CHANGELOG.md` follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/). Add your
entry to `[Unreleased]` in the same PR as the change — a release cut only renames the
heading, it does not go hunting for missing entries.

### What earns an entry

| Write one | Skip it |
|---|---|
| The tool returns different data | Dead-code removal |
| A filter or parameter behaves differently | Internal refactor |
| `isError` or a response shape changes | Test-only change |
| The process stops crashing | Hardening for input no caller can produce |
| A query is no longer injectable | |

"It was an obscure input" is not a reason to skip — a parameter silently ignored on half the
results is the same defect class as a wrong number. Dependency bumps get a single line under
`Changed`, collapsed across PRs: `Bumped 'x/y' 1.2.3 → 1.2.5 (#231, #272)`.

### Format

- Sections are `### Added`, `### Fixed`, `### Changed`, in that order. This is a deliberate
  local override of Keep a Changelog's ordering — don't "fix" it. One contiguous run of
  bullets per section, no blank lines between them.
- Newest first: a new bullet goes at the **top** of its section. After merging main into
  your branch, put your entry above the one you merged in.
- End every bullet with the **PR** number, not the issue it closes: `… (#274).` A reader
  needs the diff, and the issue is one click from the PR.
- State the observable behavior and the mechanism behind it. These entries are read by
  people debugging a version difference, so "fixed a bug in get_foo" is useless — say what
  was wrong, what they saw, and what they see now.

### Cutting a release

Rename `## [Unreleased]` to `## [X.Y.Z] - YYYY-MM-DD`, add a fresh empty `[Unreleased]`
above it, and bump `version` in `package.json` — that bump is what the release workflow
detects. Minor when tools or resources are added, patch for fixes alone; mark any breaking
change with a leading `**Breaking:**`.
