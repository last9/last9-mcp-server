# Last9 MCP Server

Domain language for the Last9 Model Context Protocol server — tools that AI agents use to query Last9 observability data.

## Language

**Toolset**:
A named server-side subset of tools. Only tools in the selected toolset(s) appear in `tools/list`. Operators choose toolsets so clients do not pay description tokens for unused tools.
_Avoid_: pack, pack filter, soft suggestion, client-gateway mass tool disable (that is a host workaround, not a toolset)

**Served tool surface**:
The exact set of tools returned by `tools/list` for the current session/configuration — what clients and `dump-tools` observe.
_Avoid_: registered tools (internal registration may be larger than what is served)

**Toolset selection**:
How operators choose which toolsets are active. For this server, selection is CLI flags and/or environment variables only — not MCP initialize parameters. Unknown toolset names fail fast (startup / `dump-tools` error listing valid names).
_Avoid_: initialize-option toolsets (deferred); warn-and-ignore unknown names; silent fallback to `all`

**Default served surface**:
When no toolset selection is set, the served tool surface is **all** registered tools (today’s behavior). Smaller toolsets are opt-in.
_Avoid_: curated-default, investigate-default (those are explicit selections, not the unset case)

**Tool description**:
The always-served text on a tool in `tools/list`: a short firing blurb (what / when / returns) plus **critical rules** needed for correct calls. This is what every client pays tokens for.
_Avoid_: treating `_base.md` + appended instructions as “disclosure” — both still compose into the tool description today

**Critical rules**:
Must-follow constraints in the tool description without which agents invent invalid queries (e.g. logjson/trace DSL shape, units, no bare fields, no invented SQL). Remain on-tool even after progressive disclosure. Cross-cutting only — per-argument constraints belong on the input schema.
_Avoid_: burying these only in optional prompts/resources; dumping per-param docs into the tool description essay

**Parameter schema description**:
Per-argument guidance on the tool input schema (`jsonschema:` tags): ranges, enums, formats, defaults. Complements a short tool description.
_Avoid_: leaving schema fields undescribed after shortening the tool description

**Tool reference**:
Long DSL manuals, stage catalogs, and deep examples that used to bloat tool descriptions. Served as MCP **resources** (not in `tools/list`). Investigation **prompts** may use these resources; prompts are not the storage for the manual.
_Avoid_: stuffing manuals into the tool description; using prompts as a dump for reference docs

**Named toolsets** (v1):
Domain groups that partition the served tool surface: `logs`, `traces`, `metrics`, `alerts`, `dashboards`, plus `all` and the composite `investigate` (`logs`+`traces`+`metrics`+discovery helpers, no dashboard/alert writes). Operators may combine domain toolsets.
_Avoid_: per-tool toggles as the primary UX; write-only toolset as a v1 requirement

**Description whales**:
The tools whose manuals dominate description tokens and undergo progressive disclosure together: `get_logs`, `get_traces`, and `get_service_logs`.
_Avoid_: treating every slightly long description as a whale; `get_service_traces` is not a whale by default

**Attribute discovery**:
Learning which log/trace field names exist for an org. Done via dedicated discovery tools (`get_log_attributes`, `get_log_attributes_for_pipeline`, and trace analogues) — not by injecting the full catalog into the tool description.
_Avoid_: `{{labels}}` full-catalog injection into served tool descriptions; attribute-name allowlists in descriptions

**Description token budget**:
Acceptance ceilings on sum of served tool description sizes (not full JSON including schemas), measured via `dump-tools`: **all** surface ≤ ~12k tokens; **investigate** toolset ≤ ~10k tokens (vs ~28k all today).
_Avoid_: gating only on full `tools/list` JSON bytes (schemas move independently)

**Canonical tool snapshot**:
The `dump-tools` output with unset toolsets (`all` surface). Eval harness and CI contract diffs use this. `dump-tools` also honors toolset selection so the `investigate` budget can be measured.
_Avoid_: a separate hand-maintained tools.json that drifts from the server

## Decisions recorded

- ENG-1510 — toolsets hard-filter `tools/list`; whale manuals move to MCP resources

## Flagged ambiguities

None yet.

## Example dialogue

Dev: An automation host is paying tens of thousands of tokens just to list tools.  
Expert: Put them on the **investigate** toolset so the **served tool surface** drops. Unset still means **default served surface** = all.  
Dev: And we still need get_logs to stop inventing SQL.  
Expert: Keep **critical rules** in the **tool description**; move the DSL manual to **tool reference**.  
Dev: What about the description token budget?  
Expert: Gate on description size — **all** under ~12k tokens, **investigate** under ~10k.  
Dev: Where does the logjson manual go?  
Expert: A **tool reference** resource. Keep **critical rules** on the **tool description**; put ranges on the **parameter schema description**.  
Dev: Is get_service_logs a whale too?  
Expert: Yes — with get_logs and get_traces it's in the **description whales** set.  
Dev: What if someone typos the toolset name?  
Expert: **Toolset selection** fails fast — don't silently serve the wrong surface.
