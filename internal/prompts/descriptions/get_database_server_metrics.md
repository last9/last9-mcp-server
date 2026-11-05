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

Output: every key metric of an available database is listed in `metrics`; a key whose query returned no series (or only NaN/Inf samples) is `null` and also named in `missing_metrics`, so a missing exporter metric is not mistaken for an unqueried one.

Example metrics:
- Oracle (oracledb exporter), each an object keyed by `oracledb_instance_name`: active_sessions, executions_per_sec, user_commits_per_sec, user_rollbacks_per_sec, parse_calls_per_sec, hard_parses_per_sec, logical_reads_per_sec, consistent_gets_per_sec, db_block_gets_per_sec, physical_reads_per_sec, cpu_time_seconds_per_sec, tablespace_used_pct (max per instance).
- Aerospike: open_connections, memory_free_pct, namespace_memory_free_pct, namespace_memory_used_bytes, reads_per_sec, writes_per_sec, errors_per_sec, disk_available_pct.