# Investigation Framework

Shared conventions for all SRE/DevOps skills. Every investigation follows this framework.

## Tool Selection Priority

Use tools in this order. Each tier provides more context but is slower or noisier:

1. **Knowledge Graph** — `search_knowledge_graph` for prior RCAs, known failure modes, ownership, topology already discovered. Always start here.
2. **APM Service Tools** — `get_service_summary`, `get_service_performance_details`, `get_service_operations_summary`, `get_service_dependency_graph`, `get_service_environments`. Structured, pre-aggregated data.
3. **Alerts & Change Events** — `get_alerts`, `get_change_events`. Correlate with deployments and active alerting rules.
4. **Exceptions & Traces** — `get_exceptions`, `get_service_traces`, `get_traces`. Drill into specific request paths and error patterns. Use pipeline filters for targeted queries.
5. **Prometheus** — `prometheus_range_query`, `prometheus_instant_query`, `discover_metrics`, `prometheus_labels`, `prometheus_label_values`. Infrastructure metrics, custom app metrics, KSM metrics.
6. **kubectl** — Direct cluster inspection: `kubectl get`, `kubectl describe`, `kubectl top`, `kubectl logs` (container logs only, not Last9 logs). Use for real-time K8s state.
7. **Logs (Last Resort)** — `get_service_logs`, `get_logs`. Use only with targeted `severity_filters` and `body_filters` derived from earlier tiers. Never start with logs.

## Time Range Conventions

- **Default investigation window**: 1 hour before the reported issue time
- **Baseline comparison**: Same duration, immediately preceding the investigation window (e.g., if investigating 14:00-15:00, baseline is 13:00-14:00)
- **Change event correlation**: 24 hours before incident start
- **Prometheus step intervals**: 1m for < 1h windows, 5m for 1-6h, 15m for 6-24h, 1h for > 24h
- **All timestamps**: Use ISO 8601 format or Unix epoch seconds as required by each tool

## Repo Context Scan Pattern

Every skill includes an early step to scan the project repository. The purpose is to compare *intended configuration* (in code) against *actual runtime behavior* (from telemetry).

### K8s / Infrastructure Files
Search for:
- `**/k8s/**`, `**/manifests/**`, `**/deploy/**`, `**/charts/**`, `**/helm/**`
- `Dockerfile*`, `Kustomization*`, `skaffold.yaml`
- `*.yaml` files containing K8s resource kinds (`Deployment`, `Service`, `HorizontalPodAutoscaler`, `PodDisruptionBudget`, `Ingress`, `ConfigMap`, `Secret`)

Extract: resource requests/limits, HPA min/max/target, replica counts, health probe configs (paths, timeouts, thresholds), node selectors/affinities/tolerations, PDB minAvailable/maxUnavailable, image versions.

### Application Config Files
Search for:
- Connection pool settings: DB pool size, max idle, max open, connection timeout
- Retry/circuit-breaker configs: max retries, backoff, circuit-breaker thresholds
- Service URLs/endpoints, environment variables
- Queue consumer configs: batch size, concurrency, poll interval
- ORM/query files, migration files
- API route definitions
- `.env*` files (note: don't commit these)

### Cross-Repo Context
After scanning the current repo, determine if a companion repo is needed:
- **If primarily application code** (Go/Python/Java/JS source, no K8s manifests): Ask for the IaC/infrastructure repo path.
- **If primarily IaC/infrastructure** (Helm charts, K8s manifests, Terraform): Ask for the application code repo path.
- If the user provides a path, read relevant configs from it. If they decline, note the gap and proceed.

### RCA-Specific: Recent Changes
For incident RCA, also run:
- `git log --oneline --since=<24h before incident>` to identify recent code changes
- Cross-reference git commits with `get_change_events` deployment records

## Knowledge Graph Discipline

### When to Ingest
- **After `get_service_dependency_graph`**: Always ingest via `ingest_knowledge` with `raw_text` containing the JSON response. The pipeline auto-extracts nodes and edges.
- **After `get_service_summary`**: Ingest if new services are discovered that aren't in the knowledge graph.
- **After `get_service_operations_summary`**: Ingest to capture endpoint/DB/messaging topology.
- **After Prometheus topology queries**: Ingest KSM metrics output to capture K8s topology (pods, containers, deployments, namespaces).

### When to Add Notes
- **After completing an RCA** (mandatory for incident-rca skill)
- **When discovering a failure mode** (e.g., "OOM kills when > 500 concurrent connections")
- **When identifying ownership** (e.g., "team-platform owns the HPA config for service X")
- **When finding config drift** from schema expectations
- **When a workaround or known issue is identified**

### Node ID Conventions
- `svc:<name>` — Services
- `db:<type>:<host>` or `datastoreinstance:<type>:<host>` — Databases
- `pod:<namespace>/<name>` — Pods
- `ns:<namespace>` — Namespaces
- `endpoint:<service>:<span_name>` — HTTP endpoints / operations
- `deploy:<namespace>/<name>` — Deployments
- `container:<namespace>/<pod>/<container>` — Containers
- `kafkatopic:<name>` — Kafka/messaging topics

### Note Conventions
- Title should be descriptive and searchable (e.g., "RCA: Frontend latency spike 2024-01-15", "Known issue: Redis connection pool exhaustion under load")
- Body in markdown format
- Always link to relevant node IDs and edge refs
- Include `edge_refs` for relationship-specific findings (e.g., a note about latency between service A and B should reference the CALLS edge)

## Common PromQL Patterns

### RED Metrics (Request, Error, Duration)
```promql
# Request rate
sum(rate(http_requests_total{service="$SVC"}[5m]))

# Error rate
sum(rate(http_requests_total{service="$SVC",status=~"5.."}[5m]))
  / sum(rate(http_requests_total{service="$SVC"}[5m]))

# Duration (p95)
histogram_quantile(0.95, sum(rate(http_request_duration_seconds_bucket{service="$SVC"}[5m])) by (le))
```

### USE Metrics (Utilization, Saturation, Errors)
```promql
# CPU utilization
sum(rate(container_cpu_usage_seconds_total{pod=~"$POD.*"}[5m]))
  / sum(kube_pod_container_resource_requests{pod=~"$POD.*",resource="cpu"})

# Memory utilization
sum(container_memory_working_set_bytes{pod=~"$POD.*"})
  / sum(kube_pod_container_resource_requests{pod=~"$POD.*",resource="memory"})

# Saturation (CPU throttling)
sum(rate(container_cpu_cfs_throttled_periods_total{pod=~"$POD.*"}[5m]))
  / sum(rate(container_cpu_cfs_periods_total{pod=~"$POD.*"}[5m]))
```

## Confirm Before Concluding

Every skill includes a "confirm with user" gate before final recommendations:
1. Summarize findings discovered so far
2. Identify specific gaps or assumptions made
3. Ask targeted questions (not open-ended "anything else?")
4. Wait for user response before proceeding to recommendations

The user often knows context not captured in telemetry: planned maintenance, known legacy behavior, team decisions, partial rollouts, feature flags, traffic patterns.
