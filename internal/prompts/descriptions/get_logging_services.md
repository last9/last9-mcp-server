# get_logging_services Tool Arguments

## Purpose

Discover which services are actively sending logs to Last9. Returns the exact `service_name`,
`env`, `physical_index`, and `severity` values present in the log ingestion pipeline.

Call this **before** `get_logs` or `get_service_logs` to:
- Confirm a service is actually ingesting logs
- Get the exact spelling of `service_name` and `env` (prevents silent empty results)
- Obtain the `physical_index` to pass as the `index` parameter for faster log queries
- Know which severity levels are present for a service

## Parameters

- `service` (string, optional): Filter by service name. Omit to list all services sending logs.
- `env` (string, optional): Filter by environment (e.g. `production`, `staging`). Omit for all environments.

Call with no parameters to get a complete map of all services and their log indexes.

## Output

Returns a JSON array of entries. Each entry has:
- `service_name`: exact service identifier to use in log queries
- `env`: environment label
- `physical_index`: index string to pass as `index` param (e.g. `physical_index:payments`)
- `severity`: log severity level present for this combination

## Examples

User: "which services are sending logs?"
→ `{}`

User: "is checkout service sending logs?"
→ `{"service": "checkout"}`

User: "which services have logs in production?"
→ `{"env": "production"}`

User: "show logging info for api in staging"
→ `{"service": "api", "env": "staging"}`

User: "before querying logs for payment-service"
→ `{"service": "payment-service"}`

User: "what index should I use for the auth service?"
→ `{"service": "auth"}`
