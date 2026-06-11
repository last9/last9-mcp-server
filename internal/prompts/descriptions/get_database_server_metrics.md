Discover and query server-side database metrics from Prometheus exporters.

Detects which database exporters are running (postgres_exporter, mysqld_exporter, redis_exporter,
oracle_exporter, mongodb_exporter, etc.) by probing for known metric prefixes, then queries key
health metrics for each detected database.

Server-side metrics provide a different perspective than client-side traces:
- Connection pool utilization (active, idle, max connections)
- Cache/buffer hit ratios
- Replication lag
- Lock contention
- Disk I/O and tablespace usage
- Query throughput from the server perspective

Parameters:
- db_system: (Optional) Focus on a specific database type (e.g. "postgresql", "mysql", "oracle", "redis", "mongodb", "mssql", "elasticsearch", "aerospike"). If omitted, discovers all available exporters.
- lookback_minutes: (Optional) Time window in minutes (default: 60).
- start_time_iso: (Optional) Start time in RFC3339 format.
- end_time_iso: (Optional) End time in RFC3339 format.

Example metrics:
- Aerospike: open_connections, memory_free_pct, namespace_memory_free_pct, namespace_memory_used_bytes, reads_per_sec, writes_per_sec, errors_per_sec, disk_available_pct.