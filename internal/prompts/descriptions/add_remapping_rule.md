Create a remapping rule in the Last9 control plane (ingestion-time transform, before storage).

Each call creates **one** rule. Map rules (`logs_map`, `traces_map`) need a separate call per `target_attribute` — the UI batches these, the API does not.

## Workflow

1. Call `get_remapping_rules` for the target `rule_type` — check existing names and avoid duplicates.
2. Discover source fields before writing:
   - **logs_map / traces_map**: call `get_log_attributes` (or `get_trace_attributes` for traces) to find real attribute paths.
   - **logs_extract (json)**: call `get_logs` on sample lines to identify JSON field names in the body (e.g. `level`, not `severity` on the attribute).
   - **logs_extract (pattern)**: inspect raw log bodies in `get_logs` to craft the regex.
3. Call `add_remapping_rule` with a unique `name`.

## All types

Required: `rule_type`, `name`, `remap_keys`, `target_attribute`.

Optional: `region` (defaults to configured datasource region).

## logs_extract

Extract fields from log line **bodies** into log or resource attributes.

- `extract_type` (required): `json` or `pattern`
- `target_attribute` (required): `log_attributes` or `resource_attributes`
- `action` (optional): `upsert` (default, replace existing values) or `insert` (keep history)
- `prefix` (optional, json only): prepended to extracted field names (e.g. `ec2_` → `ec2_id`)
- `preconditions` (optional): at most **one** scope filter. Omit for all lines; include one to match specific lines only.

### remap_keys for logs_extract

**json** — plain JSON field names from the log body, **not** `attributes["..."]` paths:

```json
["level"]
["app_team", "app_name"]
```

**pattern** — exactly **one** regex with named capture groups:

```json
["\\[(?P<severity>DEBUG|INFO|WARN|ERROR)[ |]"]
```

### preconditions (logs_extract only)

Scope extraction to matching lines. Keys use attribute paths:

- Log attributes: `attributes["key_name"]`
- Resource attributes: `resource.attributes["key_name"]`

Operators: `equals`, `not_equals`, `like` (`like` value must be valid regex).

Omit `preconditions` to apply to all lines (may increase ingestion delay — prefer scoping when possible).

## logs_map

Promote existing attributes to standardized log fields. **One rule per target.**

- `target_attribute` (required): `service`, `severity`, or `resource_deployment.environment`
- `remap_keys`: attribute paths, evaluated **left to right** — first non-empty value wins
- Do not pass `extract_type`, `action`, `prefix`, or `preconditions`

Example — map service from two fallback attributes:

```json
{
  "rule_type": "logs_map",
  "name": "map-service-from-attrs",
  "target_attribute": "service",
  "remap_keys": ["attributes[\"service_name\"]", "attributes[\"app_name\"]"]
}
```

## traces_map

Promote trace attributes to standardized service name. **One rule per target.**

- `target_attribute` (required): `service`
- `remap_keys`: attribute paths, left-to-right priority (first non-empty wins)
- Do not pass `extract_type`, `action`, `prefix`, or `preconditions`

Example:

```json
{
  "rule_type": "traces_map",
  "name": "map-trace-service",
  "target_attribute": "service",
  "remap_keys": ["resource.attributes[\"service.name\"]", "attributes[\"service\"]"]
}
```
