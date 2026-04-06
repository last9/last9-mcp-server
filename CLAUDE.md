# last9-mcp-server

## Eval harness

Evals for this MCP server live at `~/Projects/last9-mcp-evals`.

Run exception investigation evals:
```
cd ~/Projects/last9-mcp-evals && npm run eval:exception
```

Run all evals:
```
cd ~/Projects/last9-mcp-evals && npm run eval
```

The eval harness reads tool descriptions directly from this repo at runtime via `DESCRIPTION_PATHS` in `src/runner.ts`. Editing a `.md` file under `internal/prompts/descriptions/` takes effect immediately on the next eval run — no rebuild needed.

### Eval operations and their description files

| Operation | Description file |
|-----------|-----------------|
| `trace_query` | `internal/prompts/descriptions/get_traces.md` |
| `log_query` | `internal/prompts/descriptions/get_logs.md` |
| `get_alerts` | `internal/prompts/descriptions/get_alerts.md` |
| `get_alert_config` | `internal/prompts/descriptions/get_alert_config.md` |
| `exception_investigation` | `internal/prompts/descriptions/exception_investigation.md` |

When fixing a description bug: edit the relevant `.md` file under `internal/prompts/descriptions/`. The live MCP server embeds these files at compile time; the eval harness reads them at runtime. One file, one edit.
