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

- Define each tool's `Args` struct in the tool's own handler file, alongside its `New<Tool>Handler` (e.g. `GetFooArgs` in `foo/get_foo.go`). This is the repo-wide convention across every package (`alerting`, `apm`, `telemetry/logs`, `telemetry/traces`, and `dashboards`' own `get.go`/`list.go`).
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
