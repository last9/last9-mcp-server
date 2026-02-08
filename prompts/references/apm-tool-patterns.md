# APM Tool Patterns

Decision tree and scenario playbooks for Last9 APM tools.

## Tool Selection Decision Tree

```
Is the issue about a specific service?
├─ No → get_service_summary (all services) → identify affected service(s)
└─ Yes
   ├─ Do you know the environment?
   │  ├─ No → get_service_environments → pick environment
   │  └─ Yes → continue
   ├─ What's the symptom?
   │  ├─ General performance → get_service_performance_details
   │  ├─ Specific endpoint/operation → get_service_operations_summary
   │  ├─ Dependency issue → get_service_dependency_graph
   │  ├─ Errors/exceptions → get_exceptions
   │  └─ Unknown → get_service_performance_details first
   ├─ Need request-level detail?
   │  ├─ Know the trace attributes → get_traces (pipeline filters)
   │  └─ Service-scoped → get_service_traces
   └─ Need infra metrics?
      ├─ Know the metric name → prometheus_range_query / prometheus_instant_query
      └─ Don't know → discover_metrics → prometheus_labels → prometheus_label_values
```

## Scenario Playbooks

### Scenario: Service Is Slow

**Symptoms**: High p95/p99, low apdex, user complaints about latency.

1. `get_service_performance_details` — Confirm latency spike in p50/p90/p95/p99. Compare against baseline (preceding equal-duration window).
2. `get_service_operations_summary` — Identify which operations are slow:
   - HTTP endpoints: Check individual p95 values, find outliers
   - DB operations: Check `db_system`, `net_peer_name`, per-query latency
   - Messaging: Check consumer operation throughput
3. `get_service_dependency_graph` — Check if downstream dependency is the bottleneck (compare outgoing call latency with baseline).
4. `get_change_events` (24h window) — Any recent deployment or config change?
5. `get_exceptions` — Check for timeout exceptions, connection errors.
6. `get_traces` with duration filter — Sample slow traces to find the slow span.
7. Prometheus: Check CPU throttling, memory pressure, connection pool saturation.
8. `get_service_logs` (last resort) — Filter for timeout or slow query log patterns.

### Scenario: Error Rate Spike

**Symptoms**: Increased 5xx responses, error percentage above baseline.

1. `get_service_performance_details` — Confirm error rate spike. Note throughput changes (did traffic also spike?).
2. `get_exceptions` — Get exception types, messages, frequencies. Group by exception class.
3. `get_service_operations_summary` — Which endpoints have elevated errors?
4. `get_change_events` (24h) — Deployment correlation. Was there a recent deploy?
5. `get_service_dependency_graph` — Is the error from a downstream? Check outgoing error rates.
6. `get_traces` with error filter — Sample error traces, examine span error attributes.
7. `get_alerts` — Any active alerts that correlate?
8. `get_service_logs` (last resort) — Filter `severity_filters: ["ERROR", "FATAL"]` with error message patterns from exceptions.

### Scenario: DB Connection Issues

**Symptoms**: Connection pool exhaustion, query timeouts, "too many connections" errors.

1. `get_service_operations_summary` — Find DB operations by `db_system` and `net_peer_name`. Check per-query latency and throughput.
2. `get_exceptions` — Look for connection timeout, pool exhaustion, deadlock exceptions.
3. Prometheus connection pool metrics (names vary by framework):
   ```promql
   # Go sql.DB pool
   go_sql_open_connections{db_name="$DB"}
   go_sql_idle_connections{db_name="$DB"}
   go_sql_wait_count_total{db_name="$DB"}
   go_sql_wait_duration_seconds_total{db_name="$DB"}

   # HikariCP (Java)
   hikaricp_connections_active{pool="$POOL"}
   hikaricp_connections_idle{pool="$POOL"}
   hikaricp_connections_pending{pool="$POOL"}
   hikaricp_connections_timeout_total{pool="$POOL"}
   ```
   Use `discover_metrics` if unsure about metric names.
4. `get_service_traces` with slow duration filter — Find traces with slow DB spans.
5. **Scan project repo** for connection pool config: pool size, max idle, connection timeout, max lifetime.
6. Compare configured pool size against observed connection count from Prometheus.
7. `get_service_logs` (last resort) — Filter for "connection refused", "pool exhausted", "too many connections".

### Scenario: Kafka Consumer Lag

**Symptoms**: Growing consumer lag, delayed message processing, stale data.

1. `get_service_operations_summary` — Find messaging operations by `messaging_system`. Check consumer throughput.
2. Prometheus consumer lag metrics:
   ```promql
   # Standard Kafka consumer lag
   kafka_consumer_group_lag{consumergroup="$CG", topic="$TOPIC"}

   # Kafka consumer lag sum
   sum by (consumergroup) (kafka_consumer_group_lag{consumergroup="$CG"})

   # Consumer offset rate (processing rate)
   rate(kafka_consumer_group_current_offset{consumergroup="$CG", topic="$TOPIC"}[5m])

   # Producer rate (incoming rate)
   rate(kafka_topic_partition_current_offset{topic="$TOPIC"}[5m])
   ```
   Use `discover_metrics` if metric names differ (e.g., Redpanda, Confluent).
3. Compare producer rate vs consumer rate — is the consumer keeping up?
4. `get_service_performance_details` — Check consumer service throughput and errors.
5. `get_change_events` — Any recent deployment of the consumer service?
6. **Scan project repo** for consumer config: batch size, concurrency/parallelism, poll interval, max poll records.
7. `get_service_logs` (last resort) — Filter for rebalance events, deserialization errors, processing failures.

## Time Range Conventions

| Investigation Scope | Range | Step |
|---|---|---|
| Active incident (ongoing) | Last 1h | 1m |
| Recent incident (< 6h ago) | Incident window + 1h before | 5m |
| Historical investigation | Custom window | 15m |
| Baseline comparison | Equal duration before incident | Same as incident |
| Change correlation | 24h before incident start | 15m |

## APM Tool Output Interpretation

### `get_service_performance_details` Fields
- **Throughput**: Requests per minute. Compare with baseline to detect traffic changes.
- **ErrorRate**: Percentage. Distinguish between 4xx (client) and 5xx (server) if trace data available.
- **P50/P90/P95/P99**: Response time percentiles in milliseconds. P50 is median, P99 captures tail latency.
- **Apdex**: 0-1 score. Below 0.85 indicates degraded user experience. Below 0.5 is unacceptable.
- **Availability**: Percentage of successful responses. Factor in expected error rates.

### `get_service_operations_summary` Key Patterns
- **HTTP endpoints**: Listed by span name (e.g., `GET /api/users`). High latency on specific endpoints narrows the search.
- **DB queries**: Identified by `db_system` (postgres, mysql, redis) and `net_peer_name`. Per-query latency reveals slow queries.
- **Messaging**: Identified by `messaging_system` (kafka, rabbitmq). Consumer throughput vs producer rate reveals lag.

### `get_service_dependency_graph` Interpretation
- **Incoming services**: Who calls this service. If one caller has much higher error rate, the problem may be in their request pattern.
- **Outgoing services**: What this service calls. High latency on an outgoing call means the issue is in the dependency, not this service.
- **Databases/Messaging**: Direct connections. Compare connection latency with historical baseline.
