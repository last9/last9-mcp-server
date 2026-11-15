package apm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"

	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type GetDatabaseServerMetricsArgs struct {
	DBSystem        string  `json:"db_system,omitempty" jsonschema:"Focus on a specific database type (e.g. postgresql, mysql, oracle, redis, mongodb, mssql, elasticsearch, aerospike). Aerospike metrics include open_connections, memory_free_pct, namespace_memory_free_pct, namespace_memory_used_bytes, reads_per_sec, writes_per_sec, errors_per_sec, disk_available_pct"`
	LookbackMinutes float64 `json:"lookback_minutes,omitempty" jsonschema:"Minutes to look back (default: 60)"`
	StartTimeISO    string  `json:"start_time_iso,omitempty" jsonschema:"Start time in RFC3339 format"`
	EndTimeISO      string  `json:"end_time_iso,omitempty" jsonschema:"End time in RFC3339 format"`
}

// dbExporterConfig defines known metric prefixes and key queries for each database exporter type.
type dbExporterConfig struct {
	DisplayName    string
	MetricPrefixes []string // prefixes to probe for existence
	KeyMetrics     []dbKeyMetric
}

type dbKeyMetric struct {
	Name    string // human-readable metric name
	Query   string // PromQL query
	ByLabel string // when set, the value maps each series' ByLabel value to its sample
}

// Oracle keys are per instance so a host running several instances is not merged.
// Names follow the oracledb exporter, as the Last9 infrastructure database queries do.
func oracleKeyMetric(name, query string) dbKeyMetric {
	return dbKeyMetric{Name: name, Query: query, ByLabel: "oracledb_instance_name"}
}

func oracleRateKeyMetric(name, counter string) dbKeyMetric {
	return oracleKeyMetric(name, fmt.Sprintf("sum by (oracledb_instance_name) (rate(%s[5m]))", counter))
}

var dbExporterConfigs = map[string]dbExporterConfig{
	"postgresql": {
		DisplayName:    "PostgreSQL",
		MetricPrefixes: []string{"pg_", "postgres_", "postgresql_"},
		KeyMetrics: []dbKeyMetric{
			{Name: "active_connections", Query: "sum(pg_stat_activity_count)"},
			{Name: "max_connections", Query: "pg_settings_max_connections"},
			{Name: "connection_utilization_pct", Query: "100 * sum(pg_stat_activity_count) / pg_settings_max_connections"},
			{Name: "cache_hit_ratio_pct", Query: "100 * sum(pg_stat_database_blks_hit) / (sum(pg_stat_database_blks_hit) + sum(pg_stat_database_blks_read) + 1)"},
			{Name: "transactions_per_sec", Query: "sum(rate(pg_stat_database_xact_commit[5m])) + sum(rate(pg_stat_database_xact_rollback[5m]))"},
			{Name: "rollback_ratio_pct", Query: "100 * sum(rate(pg_stat_database_xact_rollback[5m])) / (sum(rate(pg_stat_database_xact_commit[5m])) + sum(rate(pg_stat_database_xact_rollback[5m])) + 0.001)"},
			{Name: "deadlocks_total", Query: "sum(pg_stat_database_deadlocks)"},
			{Name: "replication_lag_seconds", Query: "max(pg_replication_lag)"},
			{Name: "database_size_bytes", Query: "sum(pg_database_size_bytes)"},
			{Name: "idle_in_transaction", Query: "sum(pg_stat_activity_count{state='idle in transaction'})"},
		},
	},
	"mysql": {
		DisplayName:    "MySQL",
		MetricPrefixes: []string{"mysql_", "mysqld_"},
		KeyMetrics: []dbKeyMetric{
			{Name: "active_connections", Query: "mysql_global_status_threads_connected"},
			{Name: "max_connections", Query: "mysql_global_variables_max_connections"},
			{Name: "connection_utilization_pct", Query: "100 * mysql_global_status_threads_connected / mysql_global_variables_max_connections"},
			{Name: "buffer_pool_hit_ratio_pct", Query: "100 * (1 - mysql_global_status_innodb_buffer_pool_reads / (mysql_global_status_innodb_buffer_pool_read_requests + 1))"},
			{Name: "queries_per_sec", Query: "rate(mysql_global_status_queries[5m])"},
			{Name: "slow_queries_per_sec", Query: "rate(mysql_global_status_slow_queries[5m])"},
			{Name: "replication_lag_seconds", Query: "mysql_slave_status_seconds_behind_master"},
			{Name: "threads_running", Query: "mysql_global_status_threads_running"},
			{Name: "table_locks_waited_per_sec", Query: "rate(mysql_global_status_table_locks_waited[5m])"},
			{Name: "aborted_connections_per_sec", Query: "rate(mysql_global_status_aborted_connects[5m])"},
		},
	},
	"oracle": {
		DisplayName:    "Oracle",
		MetricPrefixes: []string{"oracle_", "oracledb_"},
		KeyMetrics: []dbKeyMetric{
			oracleKeyMetric("active_sessions", `sum by (oracledb_instance_name) (oracledb_sessions_usage{session_status="ACTIVE"})`),
			oracleRateKeyMetric("executions_per_sec", "oracledb_executions_total"),
			oracleRateKeyMetric("user_commits_per_sec", "oracledb_user_commits_total"),
			oracleRateKeyMetric("user_rollbacks_per_sec", "oracledb_user_rollbacks_total"),
			oracleRateKeyMetric("parse_calls_per_sec", "oracledb_parse_calls_total"),
			oracleRateKeyMetric("hard_parses_per_sec", "oracledb_hard_parses_total"),
			oracleRateKeyMetric("logical_reads_per_sec", "oracledb_logical_reads_total"),
			oracleRateKeyMetric("consistent_gets_per_sec", "oracledb_consistent_gets_total"),
			oracleRateKeyMetric("db_block_gets_per_sec", "oracledb_db_block_gets_total"),
			oracleRateKeyMetric("physical_reads_per_sec", "oracledb_physical_reads_total"),
			oracleRateKeyMetric("cpu_time_seconds_per_sec", "oracledb_cpu_time_seconds_total"),
			oracleKeyMetric("tablespace_used_pct", "max by (oracledb_instance_name) (100 * oracledb_tablespace_size_usage_bytes / (oracledb_tablespace_size_limit_bytes > 0))"),
		},
	},
	"redis": {
		DisplayName:    "Redis",
		MetricPrefixes: []string{"redis_"},
		KeyMetrics: []dbKeyMetric{
			{Name: "connected_clients", Query: "sum(redis_connected_clients)"},
			{Name: "used_memory_bytes", Query: "sum(redis_memory_used_bytes)"},
			{Name: "max_memory_bytes", Query: "sum(redis_memory_max_bytes)"},
			{Name: "memory_utilization_pct", Query: "100 * sum(redis_memory_used_bytes) / (sum(redis_memory_max_bytes) + 1)"},
			{Name: "hit_rate_pct", Query: "100 * sum(redis_keyspace_hits_total) / (sum(redis_keyspace_hits_total) + sum(redis_keyspace_misses_total) + 1)"},
			{Name: "ops_per_sec", Query: "sum(rate(redis_commands_processed_total[5m]))"},
			{Name: "evicted_keys_per_sec", Query: "sum(rate(redis_evicted_keys_total[5m]))"},
			{Name: "replication_lag_seconds", Query: "max(redis_replication_delay)"},
			{Name: "blocked_clients", Query: "sum(redis_blocked_clients)"},
		},
	},
	"mongodb": {
		DisplayName:    "MongoDB",
		MetricPrefixes: []string{"mongodb_", "mongo_"},
		KeyMetrics: []dbKeyMetric{
			{Name: "current_connections", Query: "sum(mongodb_ss_connections{conn_type='current'})"},
			{Name: "available_connections", Query: "sum(mongodb_ss_connections{conn_type='available'})"},
			{Name: "ops_per_sec", Query: "sum(rate(mongodb_ss_opcounters[5m]))"},
			{Name: "cache_hit_ratio_pct", Query: "100 * mongodb_ss_wt_cache_pages_requested_from_the_cache / (mongodb_ss_wt_cache_pages_requested_from_the_cache + mongodb_ss_wt_cache_pages_read_into_cache + 1)"},
			{Name: "replication_lag_seconds", Query: "max(mongodb_mongod_replset_member_replication_lag)"},
			{Name: "page_faults_per_sec", Query: "rate(mongodb_ss_extra_info_page_faults[5m])"},
			{Name: "document_ops_per_sec", Query: "sum(rate(mongodb_ss_metrics_document[5m]))"},
			{Name: "tickets_available_read", Query: "mongodb_ss_wt_concurrentTransactions_available{type='read'}"},
			{Name: "tickets_available_write", Query: "mongodb_ss_wt_concurrentTransactions_available{type='write'}"},
		},
	},
	"mssql": {
		DisplayName:    "SQL Server",
		MetricPrefixes: []string{"mssql_", "sqlserver_"},
		KeyMetrics: []dbKeyMetric{
			{Name: "user_connections", Query: "mssql_user_connections"},
			{Name: "batch_requests_per_sec", Query: "rate(mssql_batch_requests_total[5m])"},
			{Name: "buffer_cache_hit_ratio_pct", Query: "mssql_buffer_cache_hit_ratio"},
			{Name: "page_life_expectancy_sec", Query: "mssql_page_life_expectancy"},
			{Name: "deadlocks_per_sec", Query: "rate(mssql_deadlocks_total[5m])"},
			{Name: "lock_wait_time_ms", Query: "mssql_lock_wait_time_ms"},
		},
	},
	"aerospike": {
		DisplayName:    "Aerospike",
		MetricPrefixes: []string{"aerospike_"},
		KeyMetrics: []dbKeyMetric{
			{Name: "open_connections", Query: "sum(aerospike_node_connection_open)"},
			{Name: "memory_free_pct", Query: "min(aerospike_node_memory_free)"},
			{Name: "namespace_memory_free_pct", Query: "min(aerospike_namespace_memory_free)"},
			{Name: "namespace_memory_used_bytes", Query: "sum(aerospike_namespace_memory_usage)"},
			{Name: "reads_per_sec", Query: "sum(rate(aerospike_namespace_transaction_count{type='read',result='success'}[5m]))"},
			{Name: "writes_per_sec", Query: "sum(rate(aerospike_namespace_transaction_count{type='write',result='success'}[5m]))"},
			{Name: "errors_per_sec", Query: "sum(rate(aerospike_namespace_transaction_count{result=~'error|timeout'}[5m]))"},
			{Name: "disk_available_pct", Query: "min(aerospike_namespace_disk_available)"},
		},
	},
	"elasticsearch": {
		DisplayName:    "Elasticsearch",
		MetricPrefixes: []string{"elasticsearch_", "es_"},
		KeyMetrics: []dbKeyMetric{
			{Name: "cluster_health_status", Query: "elasticsearch_cluster_health_status"},
			{Name: "active_shards", Query: "elasticsearch_cluster_health_active_shards"},
			{Name: "unassigned_shards", Query: "elasticsearch_cluster_health_unassigned_shards"},
			{Name: "jvm_heap_used_pct", Query: "max(elasticsearch_jvm_memory_used_bytes{area='heap'} / elasticsearch_jvm_memory_max_bytes{area='heap'}) * 100"},
			{Name: "indexing_rate_per_sec", Query: "sum(rate(elasticsearch_indices_indexing_index_total[5m]))"},
			{Name: "search_rate_per_sec", Query: "sum(rate(elasticsearch_indices_search_query_total[5m]))"},
			{Name: "store_size_bytes", Query: "sum(elasticsearch_indices_store_size_bytes_total)"},
		},
	},
}

var supportedDBSystems = func() string {
	keys := make([]string, 0, len(dbExporterConfigs))
	for k := range dbExporterConfigs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ", ")
}()

func NewGetDatabaseServerMetricsHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, GetDatabaseServerMetricsArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args GetDatabaseServerMetricsArgs) (*mcp.CallToolResult, any, error) {
		startTime, endTime, err := resolveTimeRange(args.StartTimeISO, args.EndTimeISO, args.LookbackMinutes)
		if err != nil {
			return nil, nil, err
		}

		// Determine which database types to check
		configsToCheck := dbExporterConfigs
		if args.DBSystem != "" {
			if expCfg, ok := dbExporterConfigs[args.DBSystem]; ok {
				configsToCheck = map[string]dbExporterConfig{args.DBSystem: expCfg}
			} else {
				return nil, nil, fmt.Errorf("unknown db_system %q. Supported: %s", args.DBSystem, supportedDBSystems)
			}
		}

		type dbResult struct {
			DBSystem       string         `json:"db_system"`
			DisplayName    string         `json:"display_name"`
			Available      bool           `json:"available"`
			Metrics        map[string]any `json:"metrics,omitempty"`
			MissingMetrics []string       `json:"missing_metrics,omitempty"`
		}

		// Probe all database types in parallel
		type probeResult struct {
			dbSys       string
			exporterCfg dbExporterConfig
			available   bool
		}
		probeResults := make([]probeResult, 0, len(configsToCheck))
		var probeMu sync.Mutex
		var probeWg sync.WaitGroup

		for dbSys, exporterCfg := range configsToCheck {
			probeWg.Add(1)
			go func(dbSys string, exporterCfg dbExporterConfig) {
				defer probeWg.Done()
				available := probeMetricPrefix(ctx, client, cfg, exporterCfg.MetricPrefixes, startTime, endTime)
				probeMu.Lock()
				probeResults = append(probeResults, probeResult{dbSys, exporterCfg, available})
				probeMu.Unlock()
			}(dbSys, exporterCfg)
		}
		probeWg.Wait()

		// For available exporters, query all key metrics in parallel
		var results []dbResult
		var resultsMu sync.Mutex
		var metricsWg sync.WaitGroup

		for _, pr := range probeResults {
			if !pr.available {
				if args.DBSystem != "" {
					results = append(results, dbResult{
						DBSystem:    pr.dbSys,
						DisplayName: pr.exporterCfg.DisplayName,
						Available:   false,
					})
				}
				continue
			}

			metricsWg.Add(1)
			go func(pr probeResult) {
				defer metricsWg.Done()
				metrics := make(map[string]any, len(pr.exporterCfg.KeyMetrics))
				var missing []string
				var metricMu sync.Mutex
				var mWg sync.WaitGroup

				for _, km := range pr.exporterCfg.KeyMetrics {
					mWg.Add(1)
					go func(km dbKeyMetric) {
						defer mWg.Done()
						val := keyMetricValue(km, queryPromInstantSeries(ctx, client, cfg, km.Query, endTime))
						metricMu.Lock()
						metrics[km.Name] = val
						if val == nil {
							missing = append(missing, km.Name)
						}
						metricMu.Unlock()
					}(km)
				}
				mWg.Wait()
				sort.Strings(missing)

				resultsMu.Lock()
				results = append(results, dbResult{
					DBSystem:       pr.dbSys,
					DisplayName:    pr.exporterCfg.DisplayName,
					Available:      true,
					Metrics:        metrics,
					MissingMetrics: missing,
				})
				resultsMu.Unlock()
			}(pr)
		}
		metricsWg.Wait()

		if len(results) == 0 {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					&mcp.TextContent{Text: apmEmptyAdvice(
						"database server-side metrics",
						apmGuidanceArgs{dbSystem: args.DBSystem, startUnix: startTime, endUnix: endTime},
						"ensure database exporters (postgres_exporter, mysqld_exporter, etc.) are running",
						"verify metrics are scraping to the configured Prometheus datasource",
					)},
				},
			}, nil, nil
		}

		response := map[string]any{
			"count":     len(results),
			"databases": results,
		}
		allUnavailable := len(results) > 0
		for _, result := range results {
			if result.Available {
				allUnavailable = false
				break
			}
		}
		if allUnavailable {
			response["hint"] = apmEmptyAdvice(
				"database server-side metrics",
				apmGuidanceArgs{dbSystem: args.DBSystem, startUnix: startTime, endUnix: endTime},
				"ensure database exporters (postgres_exporter, mysqld_exporter, etc.) are running",
				"verify metrics are scraping to the configured Prometheus datasource",
			)
		}
		jsonBytes, err := json.Marshal(response)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to marshal response: %w", err)
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: string(jsonBytes)},
			},
		}, nil, nil
	}
}

// probeMetricPrefix checks if any metrics with the given prefixes exist in Prometheus.
func probeMetricPrefix(ctx context.Context, client *http.Client, cfg models.Config, prefixes []string, startTime, endTime int64) bool {
	// Build a regex that matches any of the prefixes
	prefixRegex := strings.Join(prefixes, "|")
	matchFilter := fmt.Sprintf(`{__name__=~"(%s).*"}`, prefixRegex)

	resp, err := utils.MakePromLabelValuesAPIQuery(ctx, client, "__name__", matchFilter, startTime, endTime, cfg)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false
	}

	var values []string
	if err := json.NewDecoder(resp.Body).Decode(&values); err != nil {
		return false
	}

	return len(values) > 0
}

// queryPromInstantSeries runs a PromQL instant query and returns its series, or nil on any failure.
func queryPromInstantSeries(ctx context.Context, client *http.Client, cfg models.Config, query string, endTime int64) apiPromInstantResp {
	resp, err := utils.MakePromInstantAPIQuery(ctx, client, query, endTime, cfg)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil
	}

	var series apiPromInstantResp
	if err := json.NewDecoder(resp.Body).Decode(&series); err != nil {
		return nil
	}
	return series
}

// keyMetricValue returns nil when no series has a finite sample: JSON cannot
// encode NaN or Inf, and one would fail the whole response.
func keyMetricValue(km dbKeyMetric, series apiPromInstantResp) any {
	if km.ByLabel == "" {
		if len(series) == 0 {
			return nil
		}
		if v := parsePromValue(series[0].Value); isFinite(v) {
			return v
		}
		return nil
	}

	byLabel := make(map[string]float64, len(series))
	for _, s := range series {
		if v := parsePromValue(s.Value); isFinite(v) {
			byLabel[s.Metric[km.ByLabel]] = v
		}
	}
	if len(byLabel) == 0 {
		return nil
	}
	return byLabel
}
