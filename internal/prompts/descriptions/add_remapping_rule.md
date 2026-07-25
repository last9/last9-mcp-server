Create a remapping rule in the Last9 control plane.

Set `rule_type` to one of `logs_extract`, `logs_map`, or `traces_map`. Required for all types: `name`, `remap_keys`, and `target_attribute`.

## logs_extract

Extract fields from log line bodies into log or resource attributes.

- `extract_type` (required): `json` to parse structured JSON bodies, or `pattern` for a regex with named capture groups (e.g. `\\[(?P<severity>DEBUG|INFO|WARN|ERROR)\\s*\\|`)
- `target_attribute` (required): `log_attributes` or `resource_attributes`
- `action` (optional): `upsert` (default) or `insert`
- `prefix` (optional): prefix added to extracted field names
- `preconditions` (optional): at most one filter to scope extraction to matching lines. Operators: `equals`, `not_equals`, `like`

For JSON extraction, `remap_keys` lists the JSON field names to extract (e.g. `["level"]`).

For pattern extraction, `remap_keys` must contain exactly one regex pattern.

## logs_map

Map source attributes to standardized log fields. Evaluates `remap_keys` left to right and uses the first non-empty value.

- `target_attribute` (required): `service`, `severity`, or `resource_deployment.environment`
- Do not pass `extract_type`, `action`, `prefix`, or `preconditions`

## traces_map

Map source attributes to standardized trace service names. Evaluates `remap_keys` left to right and uses the first non-empty value.

- `target_attribute` (required): `service`
- Do not pass `extract_type`, `action`, `prefix`, or `preconditions`

## Region

Pass `region` when not configured via the datasource. Defaults to the configured datasource region.