package apm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"

	"last9-mcp/internal/deeplink"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// --- get_databases tool ---

const GetDatabasesDescription = `Discover all databases across your infrastructure with key performance metrics.

Returns a list of databases detected from trace data, including database type, host,
throughput (queries/min), p95 latency, error rate, and how many services use each database.

This tool uses OpenTelemetry trace metrics (trace_client_count, trace_client_duration) to identify
databases from spans with db_system set.

Parameters:
- env: (Optional) Filter by deployment environment (e.g. "production"). Default: all environments.
- lookback_minutes: (Optional) Time window in minutes (default: 60).
- start_time_iso: (Optional) Start time in RFC3339 format. Overrides lookback_minutes.
- end_time_iso: (Optional) End time in RFC3339 format.`

type GetDatabasesArgs struct {
	Env             string  `json:"env,omitempty" jsonschema:"Deployment environment to filter by (e.g. production)"`
	LookbackMinutes float64 `json:"lookback_minutes,omitempty" jsonschema:"Minutes to look back (default: 60, minimum: 1)"`
	StartTimeISO    string  `json:"start_time_iso,omitempty" jsonschema:"Start time in RFC3339 format"`
	EndTimeISO      string  `json:"end_time_iso,omitempty" jsonschema:"End time in RFC3339 format"`
}

type DatabaseSummary struct {
	DBSystem     string  `json:"db_system"`
	Host         string  `json:"host"`
	Throughput   float64 `json:"throughput_rpm"`
	P95Latency   float64 `json:"p95_latency_ms"`
	ErrorRate    float64 `json:"error_rate_pct"`
	ServiceCount int     `json:"service_count"`
}

func NewGetDatabasesHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, GetDatabasesArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args GetDatabasesArgs) (*mcp.CallToolResult, any, error) {
		startTime, endTime, err := resolveTimeRange(args.StartTimeISO, args.EndTimeISO, args.LookbackMinutes)
		if err != nil {
			return nil, nil, err
		}

		durationMin := (endTime - startTime) / 60
		if durationMin <= 0 {
			durationMin = 1
		}

		envFilter := ""
		if args.Env != "" {
			envFilter = fmt.Sprintf(`, env=~"%s"`, escapePromQLLabel(args.Env))
		}

		baseFilter := fmt.Sprintf(
			`span_kind=~"SPAN_KIND_CLIENT|SPAN_KIND_INTERNAL", db_system!=""%s`,
			envFilter,
		)

		// Build all PromQL queries
		throughputQuery := fmt.Sprintf(
			`sum by(db_system, net_peer_name)(sum_over_time(trace_client_count{%s}[%dm])) / %d`,
			baseFilter, durationMin, durationMin,
		)
		latencyQuery := fmt.Sprintf(
			`max by(db_system, net_peer_name)(avg_over_time(trace_client_duration{%s, quantile="p95"}[%dm])) * 1000`,
			baseFilter, durationMin,
		)
		errorCountQuery := fmt.Sprintf(
			`sum by(db_system, net_peer_name)(sum_over_time(trace_client_count{%s, status_code="STATUS_CODE_ERROR"}[%dm]))`,
			baseFilter, durationMin,
		)
		totalCountQuery := fmt.Sprintf(
			`sum by(db_system, net_peer_name)(sum_over_time(trace_client_count{%s}[%dm]))`,
			baseFilter, durationMin,
		)
		serviceCountQuery := fmt.Sprintf(
			`count by(db_system, net_peer_name)(sum by(service_name, db_system, net_peer_name)(sum_over_time(trace_client_count{%s}[%dm])))`,
			baseFilter, durationMin,
		)

		// Run all 5 PromQL queries in parallel, each writing to its own map
		var (
			throughputDBs   = make(map[string]*DatabaseSummary)
			latencyDBs      = make(map[string]*DatabaseSummary)
			serviceCountDBs = make(map[string]*DatabaseSummary)
			errorCounts     = make(map[string]float64)
			totalCounts     = make(map[string]float64)
			throughputErr   error
			warnings        []string
			warnMu          sync.Mutex
			wg              sync.WaitGroup
		)

		wg.Add(5)
		go func() {
			defer wg.Done()
			throughputErr = fetchPromAndPopulate(ctx, client, cfg, throughputQuery, endTime, throughputDBs, func(db *DatabaseSummary, val float64) {
				db.Throughput = val
			})
		}()
		go func() {
			defer wg.Done()
			if err := fetchPromAndPopulate(ctx, client, cfg, latencyQuery, endTime, latencyDBs, func(db *DatabaseSummary, val float64) {
				db.P95Latency = val
			}); err != nil {
				warnMu.Lock()
				warnings = append(warnings, "p95 latency unavailable")
				warnMu.Unlock()
			}
		}()
		go func() {
			defer wg.Done()
			fetchPromToMap(ctx, client, cfg, errorCountQuery, endTime, errorCounts)
		}()
		go func() {
			defer wg.Done()
			fetchPromToMap(ctx, client, cfg, totalCountQuery, endTime, totalCounts)
		}()
		go func() {
			defer wg.Done()
			if err := fetchPromAndPopulate(ctx, client, cfg, serviceCountQuery, endTime, serviceCountDBs, func(db *DatabaseSummary, val float64) {
				db.ServiceCount = int(val)
			}); err != nil {
				warnMu.Lock()
				warnings = append(warnings, "service count unavailable")
				warnMu.Unlock()
			}
		}()
		wg.Wait()

		if throughputErr != nil {
			return nil, nil, fmt.Errorf("failed to fetch throughput: %w", throughputErr)
		}

		// Merge results: throughput is the primary map, enrich from others
		databases := throughputDBs
		for key, latDB := range latencyDBs {
			if db, ok := databases[key]; ok {
				db.P95Latency = latDB.P95Latency
			}
		}
		for key, scDB := range serviceCountDBs {
			if db, ok := databases[key]; ok {
				db.ServiceCount = scDB.ServiceCount
			}
		}
		for key, total := range totalCounts {
			if total > 0 {
				if db, ok := databases[key]; ok {
					db.ErrorRate = (errorCounts[key] / total) * 100
				}
			}
		}

		if len(databases) == 0 {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					&mcp.TextContent{Text: "No databases found for the given parameters. Ensure services are instrumented with OpenTelemetry and have db_system span attribute set."},
				},
			}, nil, nil
		}

		// Sort by throughput descending
		result := make([]DatabaseSummary, 0, len(databases))
		for _, db := range databases {
			result = append(result, *db)
		}
		sort.Slice(result, func(i, j int) bool {
			return result[i].Throughput > result[j].Throughput
		})

		response := map[string]any{
			"count":     len(result),
			"databases": result,
		}
		if len(warnings) > 0 {
			response["_warnings"] = warnings
		}

		jsonBytes, err := json.Marshal(response)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to marshal response: %w", err)
		}

		// Build deep link
		dlBuilder := deeplink.NewBuilder(cfg.OrgSlug, cfg.ClusterID)
		dashboardURL := dlBuilder.BuildDatabasesLink()

		return &mcp.CallToolResult{
			Meta: deeplink.ToMeta(dashboardURL),
			Content: []mcp.Content{
				&mcp.TextContent{Text: string(jsonBytes)},
			},
		}, nil, nil
	}
}

// --- get_database_slow_queries tool ---

const GetDatabaseSlowQueriesDescription = `Find slow database queries from traces and logs.

Retrieves the slowest database operations by searching trace spans where db_system is set,
ordered by duration (descending). These are actual observed query executions captured by
OpenTelemetry instrumentation.

For each slow query, returns: trace ID (for drill-down), service name, operation/query pattern,
duration, database system, status, and timestamp.

Parameters:
- db_system: (Optional) Filter by database system (e.g. "postgresql", "mysql", "mongodb", "redis").
- host: (Optional) Filter by database host (net_peer_name from traces).
- service_name: (Optional) Filter by calling service name.
- env: (Optional) Filter by deployment environment.
- min_duration_ms: (Optional) Minimum query duration in milliseconds to include (default: 0, returns slowest first).
- lookback_minutes: (Optional) Time window in minutes (default: 60).
- start_time_iso: (Optional) Start time in RFC3339 format.
- end_time_iso: (Optional) End time in RFC3339 format.
- limit: (Optional) Maximum number of slow queries to return (default: 20).`

type GetDatabaseSlowQueriesArgs struct {
	DBSystem        string  `json:"db_system,omitempty" jsonschema:"Database system filter (e.g. postgresql, mysql, mongodb, redis)"`
	Host            string  `json:"host,omitempty" jsonschema:"Database host filter (net_peer_name)"`
	ServiceName     string  `json:"service_name,omitempty" jsonschema:"Calling service name filter"`
	Env             string  `json:"env,omitempty" jsonschema:"Deployment environment filter"`
	MinDurationMs   float64 `json:"min_duration_ms,omitempty" jsonschema:"Minimum query duration in milliseconds"`
	LookbackMinutes float64 `json:"lookback_minutes,omitempty" jsonschema:"Minutes to look back (default: 60, minimum: 1)"`
	StartTimeISO    string  `json:"start_time_iso,omitempty" jsonschema:"Start time in RFC3339 format"`
	EndTimeISO      string  `json:"end_time_iso,omitempty" jsonschema:"End time in RFC3339 format"`
	Limit           int     `json:"limit,omitempty" jsonschema:"Maximum results (default: 20)"`
}

type SlowQuery struct {
	Source      string  `json:"source"`                // "trace" or "log"
	TraceID     string  `json:"trace_id,omitempty"`
	SpanID      string  `json:"span_id,omitempty"`
	ServiceName string  `json:"service_name"`
	SpanName    string  `json:"span_name,omitempty"`
	DBSystem    string  `json:"db_system,omitempty"`
	DBStatement string  `json:"db_statement,omitempty"`
	DurationMs  float64 `json:"duration_ms"`
	StatusCode  string  `json:"status_code,omitempty"`
	Timestamp   string  `json:"timestamp"`
	// Log-specific fields (only present when source=log)
	DBNamespace  string `json:"db_namespace,omitempty"`
	PlanSummary  string `json:"plan_summary,omitempty"`
	QueryHash    string `json:"query_hash,omitempty"`
	DocsExamined int64  `json:"docs_examined,omitempty"`
	KeysExamined int64  `json:"keys_examined,omitempty"`
	RowsReturned int64  `json:"rows_returned,omitempty"`
}

func NewGetDatabaseSlowQueriesHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, GetDatabaseSlowQueriesArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args GetDatabaseSlowQueriesArgs) (*mcp.CallToolResult, any, error) {
		startTime, endTime, err := resolveTimeRange(args.StartTimeISO, args.EndTimeISO, args.LookbackMinutes)
		if err != nil {
			return nil, nil, err
		}

		limit := args.Limit
		if limit <= 0 {
			limit = 20
		}

		// Build trace query pipeline filters.
		// The traces API uses dot-notation for nested attributes:
		//   - span attributes: "attributes.db.system"
		//   - resource attributes: "resource.attributes.deployment.environment"
		var conditions []any

		// Filter by SPAN_KIND_CLIENT or SPAN_KIND_INTERNAL (DB operations)
		conditions = append(conditions, map[string]any{
			"$regex": []any{"SpanKind", "SPAN_KIND_CLIENT|SPAN_KIND_INTERNAL"},
		})

		if args.DBSystem != "" {
			conditions = append(conditions, map[string]any{
				"$eq": []any{"attributes.db.system", args.DBSystem},
			})
		} else {
			// No specific db_system — match any span that has db.system set
			conditions = append(conditions, map[string]any{
				"$exists": []any{"attributes.db.system"},
			})
		}

		if args.Host != "" {
			conditions = append(conditions, map[string]any{
				"$eq": []any{"attributes.net.peer.name", args.Host},
			})
		}

		if args.ServiceName != "" {
			conditions = append(conditions, map[string]any{
				"$eq": []any{"ServiceName", args.ServiceName},
			})
		}

		if args.Env != "" {
			conditions = append(conditions, map[string]any{
				"$eq": []any{"resource.attributes.deployment.environment", args.Env},
			})
		}

		if args.MinDurationMs > 0 {
			// Duration in traces is in nanoseconds
			minDurationNs := int64(args.MinDurationMs * 1_000_000)
			conditions = append(conditions, map[string]any{
				"$gte": []any{"Duration", minDurationNs},
			})
		}

		pipeline := []map[string]any{
			{
				"type":  "filter",
				"query": map[string]any{"$and": conditions},
			},
		}

		// Use milliseconds for traces API
		startMs := startTime * 1000
		endMs := endTime * 1000

		// Fetch traces and logs in parallel
		var (
			slowQueries []SlowQuery
			logQueries  []SlowQuery
			traceErr    error
			sqWg        sync.WaitGroup
		)

		sqWg.Add(2)
		go func() {
			defer sqWg.Done()
			resp, err := utils.MakeTracesJSONQueryAPI(ctx, client, cfg, pipeline, startMs, endMs, limit)
			if err != nil {
				traceErr = fmt.Errorf("failed to query slow database traces: %w", err)
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(resp.Body)
				bodyStr := string(body)
				if len(bodyStr) > 200 {
					bodyStr = bodyStr[:200] + "..."
				}
				traceErr = fmt.Errorf("traces API returned status %d: %s", resp.StatusCode, bodyStr)
				return
			}

			var rawResult map[string]any
			if err := json.NewDecoder(resp.Body).Decode(&rawResult); err != nil {
				traceErr = fmt.Errorf("failed to decode traces response: %w", err)
				return
			}
			slowQueries = extractSlowQueries(rawResult)
		}()
		go func() {
			defer sqWg.Done()
			logQueries = fetchSlowQueryLogs(ctx, client, cfg, args, startMs, endMs, limit)
		}()
		sqWg.Wait()

		if traceErr != nil {
			return nil, nil, traceErr
		}
		slowQueries = append(slowQueries, logQueries...)

		// Sort all results by duration descending
		sort.Slice(slowQueries, func(i, j int) bool {
			return slowQueries[i].DurationMs > slowQueries[j].DurationMs
		})

		// Cap to requested limit after merging
		if len(slowQueries) > limit {
			slowQueries = slowQueries[:limit]
		}

		if len(slowQueries) == 0 {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					&mcp.TextContent{Text: "No slow database queries found for the given parameters."},
				},
			}, nil, nil
		}

		// Count sources
		traceCount, logCount := 0, 0
		for _, q := range slowQueries {
			if q.Source == "log" {
				logCount++
			} else {
				traceCount++
			}
		}

		response := map[string]any{
			"count":        len(slowQueries),
			"from_traces":  traceCount,
			"from_logs":    logCount,
			"slow_queries": slowQueries,
		}

		jsonBytes, err := json.Marshal(response)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to marshal response: %w", err)
		}

		// Build deep link to traces with db_system filter
		dlBuilder := deeplink.NewBuilder(cfg.OrgSlug, cfg.ClusterID)
		dashboardURL := dlBuilder.BuildTracesLink(startMs, endMs, pipeline, "", "")

		return &mcp.CallToolResult{
			Meta: deeplink.ToMeta(dashboardURL),
			Content: []mcp.Content{
				&mcp.TextContent{Text: string(jsonBytes)},
			},
		}, nil, nil
	}
}

// --- get_database_queries tool ---

const GetDatabaseQueriesDescription = `Get top query patterns for a specific database, aggregated by operation.

Shows the most active and slowest query patterns hitting a database, grouped by span_name
(which typically contains the SQL operation or query fingerprint). For each pattern, returns
throughput (calls/min), average latency, p95 latency, and error rate.

This is useful for identifying:
- Hot queries (high throughput) that dominate database load
- Slow query patterns (high p95 latency) that need optimization
- Failing queries (high error rate) that indicate bugs or schema issues

Parameters:
- db_system: (Required) Database system (e.g. "postgresql", "mysql", "mongodb", "redis").
- host: (Optional) Database host to filter by (net_peer_name).
- env: (Optional) Deployment environment filter.
- lookback_minutes: (Optional) Time window in minutes (default: 60).
- start_time_iso: (Optional) Start time in RFC3339 format.
- end_time_iso: (Optional) End time in RFC3339 format.
- sort_by: (Optional) Sort by "throughput" (default), "latency", or "errors".`

type GetDatabaseQueriesArgs struct {
	DBSystem        string  `json:"db_system" jsonschema:"Database system (required, e.g. postgresql, mysql, mongodb, redis)"`
	Host            string  `json:"host,omitempty" jsonschema:"Database host filter (net_peer_name)"`
	Env             string  `json:"env,omitempty" jsonschema:"Deployment environment filter"`
	LookbackMinutes float64 `json:"lookback_minutes,omitempty" jsonschema:"Minutes to look back (default: 60)"`
	StartTimeISO    string  `json:"start_time_iso,omitempty" jsonschema:"Start time in RFC3339 format"`
	EndTimeISO      string  `json:"end_time_iso,omitempty" jsonschema:"End time in RFC3339 format"`
	SortBy          string  `json:"sort_by,omitempty" jsonschema:"Sort by: throughput (default), latency, or errors"`
}

type QueryPattern struct {
	SpanName   string  `json:"span_name"`
	CallsPerMin float64 `json:"calls_per_min"`
	AvgLatency float64 `json:"avg_latency_ms"`
	P95Latency float64 `json:"p95_latency_ms"`
	ErrorRate  float64 `json:"error_rate_pct"`
}

func NewGetDatabaseQueriesHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, GetDatabaseQueriesArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args GetDatabaseQueriesArgs) (*mcp.CallToolResult, any, error) {
		if args.DBSystem == "" {
			return nil, nil, fmt.Errorf("db_system parameter is required (e.g. postgresql, mysql, mongodb, redis)")
		}

		startTime, endTime, err := resolveTimeRange(args.StartTimeISO, args.EndTimeISO, args.LookbackMinutes)
		if err != nil {
			return nil, nil, err
		}

		durationMin := (endTime - startTime) / 60
		if durationMin <= 0 {
			durationMin = 1
		}

		baseFilter := buildDBBaseFilter(args.DBSystem, args.Host, args.Env)

		// 1. Throughput first (to check if any patterns exist)
		patterns := make(map[string]*QueryPattern)
		throughputQuery := fmt.Sprintf(
			`sum by(span_name)(sum_over_time(trace_client_count{%s}[%dm])) / %d`,
			baseFilter, durationMin, durationMin,
		)
		if err := fetchPromBySpanName(ctx, client, cfg, throughputQuery, endTime, patterns, func(p *QueryPattern, val float64) {
			p.CallsPerMin = val
		}); err != nil {
			return nil, nil, fmt.Errorf("failed to fetch throughput: %w", err)
		}

		if len(patterns) == 0 {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					&mcp.TextContent{Text: fmt.Sprintf("No query patterns found for db_system=%s. Ensure services are instrumented with OpenTelemetry database client spans.", args.DBSystem)},
				},
			}, nil, nil
		}

		// 2. Remaining metrics in parallel (avg latency, p95 latency, error count, total count)
		avgLatencyQuery := fmt.Sprintf(
			`avg by(span_name)(avg_over_time(trace_client_duration{%s, quantile="p50"}[%dm])) * 1000`,
			baseFilter, durationMin,
		)
		p95LatencyQuery := fmt.Sprintf(
			`avg by(span_name)(avg_over_time(trace_client_duration{%s, quantile="p95"}[%dm])) * 1000`,
			baseFilter, durationMin,
		)
		errorCountQuery := fmt.Sprintf(
			`sum by(span_name)(sum_over_time(trace_client_count{%s, status_code="STATUS_CODE_ERROR"}[%dm]))`,
			baseFilter, durationMin,
		)
		totalCountQuery := fmt.Sprintf(
			`sum by(span_name)(sum_over_time(trace_client_count{%s}[%dm]))`,
			baseFilter, durationMin,
		)

		// Each goroutine writes to its own map to avoid concurrent map writes
		var (
			avgLatencyMap = make(map[string]*QueryPattern)
			p95LatencyMap = make(map[string]*QueryPattern)
			qpErrorCounts = make(map[string]float64)
			qpTotalCounts = make(map[string]float64)
			qpWg          sync.WaitGroup
		)
		qpWg.Add(4)
		go func() {
			defer qpWg.Done()
			fetchPromBySpanName(ctx, client, cfg, avgLatencyQuery, endTime, avgLatencyMap, func(p *QueryPattern, val float64) {
				p.AvgLatency = val
			})
		}()
		go func() {
			defer qpWg.Done()
			fetchPromBySpanName(ctx, client, cfg, p95LatencyQuery, endTime, p95LatencyMap, func(p *QueryPattern, val float64) {
				p.P95Latency = val
			})
		}()
		go func() {
			defer qpWg.Done()
			fetchPromToSpanNameMap(ctx, client, cfg, errorCountQuery, endTime, qpErrorCounts)
		}()
		go func() {
			defer qpWg.Done()
			fetchPromToSpanNameMap(ctx, client, cfg, totalCountQuery, endTime, qpTotalCounts)
		}()
		qpWg.Wait()

		// Merge parallel results into the main patterns map
		for spanName, lat := range avgLatencyMap {
			if p, ok := patterns[spanName]; ok {
				p.AvgLatency = lat.AvgLatency
			}
		}
		for spanName, lat := range p95LatencyMap {
			if p, ok := patterns[spanName]; ok {
				p.P95Latency = lat.P95Latency
			}
		}
		for spanName, total := range qpTotalCounts {
			if total > 0 {
				if p, ok := patterns[spanName]; ok {
					p.ErrorRate = (qpErrorCounts[spanName] / total) * 100
				}
			}
		}

		// Convert to slice and sort
		result := make([]QueryPattern, 0, len(patterns))
		for _, p := range patterns {
			result = append(result, *p)
		}

		sortBy := args.SortBy
		if sortBy == "" {
			sortBy = "throughput"
		}
		sort.Slice(result, func(i, j int) bool {
			switch sortBy {
			case "latency":
				return result[i].P95Latency > result[j].P95Latency
			case "errors":
				return result[i].ErrorRate > result[j].ErrorRate
			default: // throughput
				return result[i].CallsPerMin > result[j].CallsPerMin
			}
		})

		// Cap at top 50 patterns
		if len(result) > 50 {
			result = result[:50]
		}

		response := map[string]any{
			"count":     len(result),
			"db_system": args.DBSystem,
			"sort_by":   sortBy,
			"queries":   result,
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

func buildDBBaseFilter(dbSystem, host, env string) string {
	filter := fmt.Sprintf(
		`span_kind=~"SPAN_KIND_CLIENT|SPAN_KIND_INTERNAL", db_system="%s"`,
		escapePromQLLabel(dbSystem),
	)
	if host != "" {
		filter += fmt.Sprintf(`, net_peer_name="%s"`, escapePromQLLabel(host))
	}
	if env != "" {
		filter += fmt.Sprintf(`, env=~"%s"`, escapePromQLLabel(env))
	}
	return filter
}

// escapePromQLLabel escapes special characters in a PromQL label value
// to prevent injection when interpolating into queries.
func escapePromQLLabel(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	return s
}

func fetchPromBySpanName(ctx context.Context, client *http.Client, cfg models.Config, query string, endTime int64, patterns map[string]*QueryPattern, setter func(*QueryPattern, float64)) error {
	resp, err := utils.MakePromInstantAPIQuery(ctx, client, query, endTime, cfg)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("PromQL query failed with status %d", resp.StatusCode)
	}

	var series apiPromInstantResp
	if err := json.NewDecoder(resp.Body).Decode(&series); err != nil {
		return err
	}

	for _, point := range series {
		spanName := point.Metric["span_name"]
		if spanName == "" {
			continue
		}
		val := parsePromValue(point.Value)
		p, ok := patterns[spanName]
		if !ok {
			p = &QueryPattern{SpanName: spanName}
			patterns[spanName] = p
		}
		setter(p, val)
	}
	return nil
}

func fetchPromToSpanNameMap(ctx context.Context, client *http.Client, cfg models.Config, query string, endTime int64, result map[string]float64) {
	fetchPromToMapByKey(ctx, client, cfg, query, endTime, result, func(m map[string]string) string {
		return m["span_name"]
	})
}

// --- get_database_server_metrics tool ---

const GetDatabaseServerMetricsDescription = `Discover and query server-side database metrics from Prometheus exporters.

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
- db_system: (Optional) Focus on a specific database type (e.g. "postgresql", "mysql", "oracle", "redis", "mongodb"). If omitted, discovers all available exporters.
- lookback_minutes: (Optional) Time window in minutes (default: 60).
- start_time_iso: (Optional) Start time in RFC3339 format.
- end_time_iso: (Optional) End time in RFC3339 format.`

type GetDatabaseServerMetricsArgs struct {
	DBSystem        string  `json:"db_system,omitempty" jsonschema:"Focus on a specific database type (e.g. postgresql, mysql, oracle, redis, mongodb)"`
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
	Name  string // human-readable metric name
	Query string // PromQL query
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
			{Name: "active_sessions", Query: "sum(oracledb_sessions_active)"},
			{Name: "session_utilization_pct", Query: "100 * sum(oracledb_sessions_active) / (oracledb_resource_current_utilization{resource_name='sessions'} + 1)"},
			{Name: "buffer_cache_hit_ratio_pct", Query: "oracledb_buffer_cachehit_ratio"},
			{Name: "tablespace_used_pct", Query: "max(oracledb_tablespace_used_percent)"},
			{Name: "wait_time_seconds", Query: "sum(rate(oracledb_wait_time_seconds[5m]))"},
			{Name: "parse_count_per_sec", Query: "rate(oracledb_activity_parse_count_total[5m])"},
			{Name: "user_commits_per_sec", Query: "rate(oracledb_activity_user_commits[5m])"},
			{Name: "physical_reads_per_sec", Query: "rate(oracledb_physical_reads[5m])"},
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
			DBSystem    string         `json:"db_system"`
			DisplayName string         `json:"display_name"`
			Available   bool           `json:"available"`
			Metrics     map[string]any `json:"metrics,omitempty"`
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
				metrics := make(map[string]any)
				var metricMu sync.Mutex
				var mWg sync.WaitGroup

				for _, km := range pr.exporterCfg.KeyMetrics {
					mWg.Add(1)
					go func(km dbKeyMetric) {
						defer mWg.Done()
						val := queryPromInstantValue(ctx, client, cfg, km.Query, endTime)
						if val != nil {
							metricMu.Lock()
							metrics[km.Name] = val
							metricMu.Unlock()
						}
					}(km)
				}
				mWg.Wait()

				resultsMu.Lock()
				results = append(results, dbResult{
					DBSystem:    pr.dbSys,
					DisplayName: pr.exporterCfg.DisplayName,
					Available:   true,
					Metrics:     metrics,
				})
				resultsMu.Unlock()
			}(pr)
		}
		metricsWg.Wait()

		if len(results) == 0 {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					&mcp.TextContent{Text: "No database server-side metrics found. Ensure database exporters (postgres_exporter, mysqld_exporter, etc.) are running and scraping to your Prometheus/Levitate instance."},
				},
			}, nil, nil
		}

		response := map[string]any{
			"count":     len(results),
			"databases": results,
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

// queryPromInstantValue runs a PromQL instant query and returns the scalar value, or nil.
func queryPromInstantValue(ctx context.Context, client *http.Client, cfg models.Config, query string, endTime int64) any {
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

	if len(series) == 0 {
		return nil
	}

	// Return the first value
	return parsePromValue(series[0].Value)
}

// --- helpers ---

// fetchSlowQueryLogs queries the logs API for entries with attributes['slow_query']='true'
// and extracts database-specific fields like plan_summary, docs_examined, etc.
// This is best-effort — returns nil on any error (traces are the primary source).
func fetchSlowQueryLogs(ctx context.Context, client *http.Client, cfg models.Config, args GetDatabaseSlowQueriesArgs, startMs, endMs int64, limit int) []SlowQuery {
	// Build log pipeline filter: attributes['slow_query'] = 'true'
	var conditions []any
	conditions = append(conditions, map[string]any{
		"$eq": []any{"attributes['slow_query']", "true"},
	})

	if args.DBSystem != "" {
		// db.system can be in attributes or resources
		conditions = append(conditions, map[string]any{
			"$or": []any{
				map[string]any{"$eq": []any{"attributes['db.system']", args.DBSystem}},
				map[string]any{"$eq": []any{"resources['db.system']", args.DBSystem}},
			},
		})
	}

	if args.Env != "" {
		conditions = append(conditions, map[string]any{
			"$eq": []any{"resources['deployment.environment']", args.Env},
		})
	}

	if args.ServiceName != "" {
		conditions = append(conditions, map[string]any{
			"$eq": []any{"ServiceName", args.ServiceName},
		})
	}

	pipeline := []map[string]any{
		{
			"type":  "filter",
			"query": map[string]any{"$and": conditions},
		},
	}

	resp, err := utils.MakeLogsJSONQueryAPI(ctx, client, cfg, pipeline, startMs, endMs, limit, "")
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil
	}

	var rawResult map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&rawResult); err != nil {
		return nil
	}

	return extractSlowQueryLogs(rawResult)
}

// extractSlowQueryLogs parses Loki streams response into SlowQuery entries.
// Slow query logs have attributes like db.operation.duration_ms, db.plan_summary, etc.
func extractSlowQueryLogs(rawResult map[string]any) []SlowQuery {
	data, ok := rawResult["data"].(map[string]any)
	if !ok {
		return nil
	}
	items, ok := data["result"].([]any)
	if !ok {
		return nil
	}

	var queries []SlowQuery
	for _, item := range items {
		streamData, ok := item.(map[string]any)
		if !ok {
			continue
		}

		// Extract stream-level labels
		streamLabels, _ := streamData["stream"].(map[string]any)

		values, ok := streamData["values"].([]any)
		if !ok {
			continue
		}

		for _, val := range values {
			entry, ok := val.([]any)
			if !ok || len(entry) < 2 {
				continue
			}

			timestamp, _ := entry[0].(string)
			message, _ := entry[1].(string)

			sq := SlowQuery{
				Source:    "log",
				Timestamp: timestamp,
			}

			// Try to parse stream labels for metadata
			if v, ok := streamLabels["service_name"].(string); ok {
				sq.ServiceName = v
			}
			if v, ok := streamLabels["severity"].(string); ok && v != "" {
				sq.StatusCode = v
			}

			// Try to parse the log message as JSON for structured slow query data
			var logBody map[string]any
			if err := json.Unmarshal([]byte(message), &logBody); err == nil {
				populateSlowQueryFromLogBody(&sq, logBody)
			} else {
				// Not JSON — use raw message as statement
				if len(message) > 500 {
					message = message[:500] + "..."
				}
				sq.DBStatement = message
			}

			// Only include if we got a meaningful duration
			if sq.DurationMs > 0 {
				queries = append(queries, sq)
			}
		}
	}

	return queries
}

func populateSlowQueryFromLogBody(sq *SlowQuery, body map[string]any) {
	// Duration: try multiple field names used by different instrumentations
	for _, key := range []string{"db.operation.duration_ms", "duration_ms", "durationMillis", "millis"} {
		if v := jsonFloat64(body, key); v > 0 {
			sq.DurationMs = v
			break
		}
	}

	// Database system
	for _, key := range []string{"db.system", "db_system"} {
		if v := jsonString(body, key); v != "" {
			sq.DBSystem = v
			break
		}
	}

	// Query/statement
	for _, key := range []string{"db.statement", "command", "query", "msg"} {
		if v := jsonString(body, key); v != "" {
			if len(v) > 500 {
				v = v[:500] + "..."
			}
			sq.DBStatement = v
			break
		}
	}

	// Span name / operation
	if v := jsonString(body, "span_name"); v != "" {
		sq.SpanName = v
	}

	// Service name (might override stream label)
	if v := jsonString(body, "service"); v != "" {
		sq.ServiceName = v
	}

	// Log-specific enrichment fields
	sq.DBNamespace = jsonString(body, "db.namespace")
	if sq.DBNamespace == "" {
		sq.DBNamespace = jsonString(body, "ns")
	}
	sq.PlanSummary = jsonString(body, "db.plan_summary")
	if sq.PlanSummary == "" {
		sq.PlanSummary = jsonString(body, "planSummary")
	}
	sq.QueryHash = jsonString(body, "db.query_hash")
	if sq.QueryHash == "" {
		sq.QueryHash = jsonString(body, "queryHash")
	}
	sq.DocsExamined = jsonInt64(body, "db.docs_examined")
	if sq.DocsExamined == 0 {
		sq.DocsExamined = jsonInt64(body, "docsExamined")
	}
	sq.KeysExamined = jsonInt64(body, "db.keys_examined")
	if sq.KeysExamined == 0 {
		sq.KeysExamined = jsonInt64(body, "keysExamined")
	}
	sq.RowsReturned = jsonInt64(body, "db.rows_affected")
	if sq.RowsReturned == 0 {
		sq.RowsReturned = jsonInt64(body, "nreturned")
	}
}

func jsonString(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func jsonFloat64(m map[string]any, key string) float64 {
	switch v := m[key].(type) {
	case float64:
		return v
	case int64:
		return float64(v)
	case int:
		return float64(v)
	default:
		return 0
	}
}

func jsonInt64(m map[string]any, key string) int64 {
	switch v := m[key].(type) {
	case float64:
		return int64(v)
	case int64:
		return v
	case int:
		return int64(v)
	default:
		return 0
	}
}

func extractSlowQueries(rawResult map[string]any) []SlowQuery {
	data, ok := rawResult["data"].(map[string]any)
	if !ok {
		return nil
	}
	items, ok := data["result"].([]any)
	if !ok {
		return nil
	}

	queries := make([]SlowQuery, 0, len(items))
	for _, item := range items {
		span, ok := item.(map[string]any)
		if !ok {
			continue
		}

		durationNs := jsonFloat64(span, "Duration")
		durationMs := durationNs / 1_000_000

		sq := SlowQuery{
			Source:      "trace",
			TraceID:     jsonString(span, "TraceId"),
			SpanID:      jsonString(span, "SpanId"),
			ServiceName: jsonString(span, "ServiceName"),
			SpanName:    jsonString(span, "SpanName"),
			DurationMs:  durationMs,
			StatusCode:  jsonString(span, "StatusCode"),
			Timestamp:   jsonString(span, "Timestamp"),
		}

		// Extract db.system and db.statement from SpanAttributes
		if attrs, ok := span["SpanAttributes"].(map[string]any); ok {
			if v, ok := attrs["db.system"].(string); ok {
				sq.DBSystem = v
			}
			if v, ok := attrs["db.statement"].(string); ok && v != "" {
				sq.DBStatement = v
				// Truncate very long SQL statements
				if len(sq.DBStatement) > 500 {
					sq.DBStatement = sq.DBStatement[:500] + "..."
				}
			}
		}

		queries = append(queries, sq)
	}
	return queries
}


// fetchPromAndPopulate runs a PromQL instant query and populates DatabaseSummary entries
// keyed by "db_system|net_peer_name".
func fetchPromAndPopulate(ctx context.Context, client *http.Client, cfg models.Config, query string, endTime int64, databases map[string]*DatabaseSummary, setter func(*DatabaseSummary, float64)) error {
	resp, err := utils.MakePromInstantAPIQuery(ctx, client, query, endTime, cfg)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("PromQL query failed with status %d", resp.StatusCode)
	}

	var series apiPromInstantResp
	if err := json.NewDecoder(resp.Body).Decode(&series); err != nil {
		return fmt.Errorf("failed to decode PromQL response: %w", err)
	}

	for _, point := range series {
		dbSystem := point.Metric["db_system"]
		host := point.Metric["net_peer_name"]
		key := dbSystem + "|" + host

		val := parsePromValue(point.Value)

		db, ok := databases[key]
		if !ok {
			db = &DatabaseSummary{
				DBSystem: dbSystem,
				Host:     host,
			}
			databases[key] = db
		}
		setter(db, val)
	}
	return nil
}

// fetchPromToMap runs a PromQL query and stores values in a map keyed by "db_system|net_peer_name".
func fetchPromToMap(ctx context.Context, client *http.Client, cfg models.Config, query string, endTime int64, result map[string]float64) {
	fetchPromToMapByKey(ctx, client, cfg, query, endTime, result, func(m map[string]string) string {
		return m["db_system"] + "|" + m["net_peer_name"]
	})
}

// fetchPromToMapByKey is the generic version: runs a PromQL instant query
// and stores values in a map using a caller-provided key extractor.
func fetchPromToMapByKey(ctx context.Context, client *http.Client, cfg models.Config, query string, endTime int64, result map[string]float64, keyFn func(map[string]string) string) {
	resp, err := utils.MakePromInstantAPIQuery(ctx, client, query, endTime, cfg)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return
	}

	var series apiPromInstantResp
	if err := json.NewDecoder(resp.Body).Decode(&series); err != nil {
		return
	}

	for _, point := range series {
		key := keyFn(point.Metric)
		if key == "" {
			continue
		}
		result[key] = parsePromValue(point.Value)
	}
}

func parsePromValue(value []any) float64 {
	if len(value) < 2 {
		return 0
	}
	switch v := value[1].(type) {
	case string:
		f, _ := strconv.ParseFloat(v, 64)
		return f
	case float64:
		return v
	default:
		return 0
	}
}
