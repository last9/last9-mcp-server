Suggests correct entity names when you're unsure of the exact spelling.

Use this tool PROACTIVELY before querying with an entity name you're not confident about — for example,
when the user types something that looks like a typo, abbreviation, or partial name.

Returns up to 3 closest matches with similarity scores (0–100%) from the Last9 catalog,
covering all entity types: services, environments, hosts, databases, k8s deployments,
k8s namespaces, jobs, and more.

When to use:
- Before calling get_service_logs, get_service_traces, get_service_performance_details, etc.
  with a service name that might be misspelled (e.g. "paymnt-svc", "prod-srvice")
- When a previous tool call returned empty results for a given entity name
- When the user says something ambiguous like "the payment thing" or "prod env"

Parameters:
- query: (Required) The name to search for — can be a partial name, misspelling, or abbreviation
- type: (Optional) Restrict suggestions to a specific entity type (e.g. "service", "environment",
  "host", "k8s_deployment", "k8s_namespace", "database", "job")

Examples:
- query="paymnt-svc"  → "payment-service (92%, service)"
- query="prod"        → "production (89%, environment)", "prod-eu (82%, environment)"
- query="payment"     → "payment-service (81%, service)", "payment-gateway (76%, service)"
- query="web-01"      → "web-01.prod (88%, host)"
- query="order-svc"   → "order-service (85%, k8s_deployment)"
