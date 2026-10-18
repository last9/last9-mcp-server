Create one remapping rule in the Last9 control plane (an ingestion-time transform, applied before storage). Map rules need one call per `target_attribute`.

## Workflow

1. Call `get_remapping_rules` for the target `rule_type` and pick a `name` not already in use.
2. Discover the real source fields first; never guess attribute paths or body field names.
3. Call `add_remapping_rule`.

On a timeout or connection error the create may or may not have been applied; check by name with get_remapping_rules before retrying.

Required for every type: `rule_type`, `name`, `remap_keys`, `target_attribute`. Optional: `region` (default: the configured datasource region).

## logs_extract

Extract fields from log line bodies into attributes.

- `extract_type` (required): `json` or `pattern`.
- `target_attribute`: `log_attributes` or `resource_attributes`.
- `remap_keys`: for `json`, plain body field names such as `["level"]`, not `attributes["..."]` paths; for `pattern`, exactly one regex with named capture groups, e.g. `["\\[(?P<severity>DEBUG|INFO|WARN|ERROR)[ |]"]`.
- `action` (optional): `upsert` (default, replace existing values) or `insert` (keep existing values).
- `prefix` (optional): prepended to extracted field names (`ec2_` → `ec2_id`).
- `preconditions` (optional): at most one `{key, value, operator}` filter; omit to apply to all lines. Keys are `attributes["k"]` or `resource.attributes["k"]`; `operator` is `equals` (default), `not_equals` or `like` (value must be a valid regex). Scope when possible: unscoped extraction can add ingestion delay.

## logs_map and traces_map

Promote existing attributes to a standard field, one rule per target.

- `target_attribute`: logs_map takes `service`, `severity` or `resource_deployment.environment`; traces_map takes `service`.
- `remap_keys`: attribute paths, evaluated left to right; the first non-empty value wins.
- Do not pass `extract_type`, `action`, `prefix` or `preconditions`.

```json
{"rule_type": "logs_map", "name": "map-service-from-attrs", "target_attribute": "service", "remap_keys": ["attributes[\"service_name\"]", "attributes[\"app_name\"]"]}
```

## Pairs well with

- `get_log_attributes`: real log attribute paths for logs_map remap_keys and logs_extract preconditions.
- `get_trace_attributes`: real trace attribute paths for traces_map remap_keys.
- `get_logs`: sample log bodies, to find JSON field names or craft a pattern regex for logs_extract.
