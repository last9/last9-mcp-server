package apm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"

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
			envFilter = fmt.Sprintf(", env=~'%s'", args.Env)
		}

		baseFilter := fmt.Sprintf(
			`span_kind=~"SPAN_KIND_CLIENT|SPAN_KIND_INTERNAL", db_system!=""%s`,
			envFilter,
		)

		// Build a map keyed by "db_system|host"
		databases := make(map[string]*DatabaseSummary)

		// 1. Throughput (rpm)
		throughputQuery := fmt.Sprintf(
			`sum by(db_system, net_peer_name)(sum_over_time(trace_client_count{%s}[%dm])) / %d`,
			baseFilter, durationMin, durationMin,
		)
		if err := fetchPromAndPopulate(ctx, client, cfg, throughputQuery, endTime, databases, func(db *DatabaseSummary, val float64) {
			db.Throughput = val
		}); err != nil {
			return nil, nil, fmt.Errorf("failed to fetch throughput: %w", err)
		}

		// 2. P95 latency (ms)
		latencyQuery := fmt.Sprintf(
			`max by(db_system, net_peer_name)(avg_over_time(trace_client_duration{%s, quantile="p95"}[%dm])) * 1000`,
			baseFilter, durationMin,
		)
		if err := fetchPromAndPopulate(ctx, client, cfg, latencyQuery, endTime, databases, func(db *DatabaseSummary, val float64) {
			db.P95Latency = val
		}); err != nil {
			// Non-fatal: latency might not be available
		}

		// 3. Error rate (%)
		errorCountQuery := fmt.Sprintf(
			`sum by(db_system, net_peer_name)(sum_over_time(trace_client_count{%s, status_code="STATUS_CODE_ERROR"}[%dm]))`,
			baseFilter, durationMin,
		)
		totalCountQuery := fmt.Sprintf(
			`sum by(db_system, net_peer_name)(sum_over_time(trace_client_count{%s}[%dm]))`,
			baseFilter, durationMin,
		)
		errorCounts := make(map[string]float64)
		totalCounts := make(map[string]float64)
		fetchPromToMap(ctx, client, cfg, errorCountQuery, endTime, errorCounts)
		fetchPromToMap(ctx, client, cfg, totalCountQuery, endTime, totalCounts)
		for key, total := range totalCounts {
			if total > 0 {
				if db, ok := databases[key]; ok {
					db.ErrorRate = (errorCounts[key] / total) * 100
				}
			}
		}

		// 4. Service count
		serviceCountQuery := fmt.Sprintf(
			`count by(db_system, net_peer_name)(sum by(service_name, db_system, net_peer_name)(sum_over_time(trace_client_count{%s}[%dm])))`,
			baseFilter, durationMin,
		)
		if err := fetchPromAndPopulate(ctx, client, cfg, serviceCountQuery, endTime, databases, func(db *DatabaseSummary, val float64) {
			db.ServiceCount = int(val)
		}); err != nil {
			// Non-fatal
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

		// Build trace query pipeline filters
		var conditions []any

		// Always filter for spans with db_system set
		conditions = append(conditions, map[string]any{
			"$neq": []any{"SpanAttributes['db.system']", ""},
		})

		// Filter by SPAN_KIND_CLIENT or SPAN_KIND_INTERNAL
		conditions = append(conditions, map[string]any{
			"$or": []any{
				map[string]any{"$eq": []any{"SpanKind", "SPAN_KIND_CLIENT"}},
				map[string]any{"$eq": []any{"SpanKind", "SPAN_KIND_INTERNAL"}},
			},
		})

		if args.DBSystem != "" {
			conditions = append(conditions, map[string]any{
				"$eq": []any{"SpanAttributes['db.system']", args.DBSystem},
			})
		}

		if args.Host != "" {
			conditions = append(conditions, map[string]any{
				"$eq": []any{"SpanAttributes['net.peer.name']", args.Host},
			})
		}

		if args.ServiceName != "" {
			conditions = append(conditions, map[string]any{
				"$eq": []any{"ServiceName", args.ServiceName},
			})
		}

		if args.Env != "" {
			conditions = append(conditions, map[string]any{
				"$eq": []any{"ResourceAttributes['deployment.environment']", args.Env},
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

		resp, err := utils.MakeTracesJSONQueryAPI(ctx, client, cfg, pipeline, startMs, endMs, limit)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to query slow database traces: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			bodyStr := string(body)
			if len(bodyStr) > 200 {
				bodyStr = bodyStr[:200] + "..."
			}
			return nil, nil, fmt.Errorf("traces API returned status %d: %s", resp.StatusCode, bodyStr)
		}

		var rawResult map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&rawResult); err != nil {
			return nil, nil, fmt.Errorf("failed to decode traces response: %w", err)
		}

		// Extract and transform spans into SlowQuery format
		slowQueries := extractSlowQueries(rawResult)

		// Enrich with slow query logs (best-effort — logs may not exist)
		logQueries := fetchSlowQueryLogs(ctx, client, cfg, args, startMs, endMs, limit)
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

		// Build patterns map keyed by span_name
		patterns := make(map[string]*QueryPattern)

		// 1. Throughput (calls/min) by span_name
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

		// 2. Avg latency (ms) by span_name
		avgLatencyQuery := fmt.Sprintf(
			`avg by(span_name)(avg_over_time(trace_client_duration{%s, quantile="p50"}[%dm])) * 1000`,
			baseFilter, durationMin,
		)
		fetchPromBySpanName(ctx, client, cfg, avgLatencyQuery, endTime, patterns, func(p *QueryPattern, val float64) {
			p.AvgLatency = val
		})

		// 3. P95 latency (ms) by span_name
		p95LatencyQuery := fmt.Sprintf(
			`avg by(span_name)(avg_over_time(trace_client_duration{%s, quantile="p95"}[%dm])) * 1000`,
			baseFilter, durationMin,
		)
		fetchPromBySpanName(ctx, client, cfg, p95LatencyQuery, endTime, patterns, func(p *QueryPattern, val float64) {
			p.P95Latency = val
		})

		// 4. Error rate by span_name
		errorCountQuery := fmt.Sprintf(
			`sum by(span_name)(sum_over_time(trace_client_count{%s, status_code="STATUS_CODE_ERROR"}[%dm]))`,
			baseFilter, durationMin,
		)
		totalCountQuery := fmt.Sprintf(
			`sum by(span_name)(sum_over_time(trace_client_count{%s}[%dm]))`,
			baseFilter, durationMin,
		)
		errorCounts := make(map[string]float64)
		totalCounts := make(map[string]float64)
		fetchPromToSpanNameMap(ctx, client, cfg, errorCountQuery, endTime, errorCounts)
		fetchPromToSpanNameMap(ctx, client, cfg, totalCountQuery, endTime, totalCounts)
		for spanName, total := range totalCounts {
			if total > 0 {
				if p, ok := patterns[spanName]; ok {
					p.ErrorRate = (errorCounts[spanName] / total) * 100
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
		dbSystem,
	)
	if host != "" {
		filter += fmt.Sprintf(`, net_peer_name="%s"`, host)
	}
	if env != "" {
		filter += fmt.Sprintf(`, env=~"%s"`, env)
	}
	return filter
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
		spanName := point.Metric["span_name"]
		if spanName == "" {
			continue
		}
		result[spanName] = parsePromValue(point.Value)
	}
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

		durationNs := extractFloat64(span, "Duration")
		durationMs := durationNs / 1_000_000

		sq := SlowQuery{
			Source:      "trace",
			TraceID:     extractStr(span, "TraceId"),
			SpanID:      extractStr(span, "SpanId"),
			ServiceName: extractStr(span, "ServiceName"),
			SpanName:    extractStr(span, "SpanName"),
			DurationMs:  durationMs,
			StatusCode:  extractStr(span, "StatusCode"),
			Timestamp:   extractStr(span, "Timestamp"),
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

func extractStr(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func extractFloat64(m map[string]any, key string) float64 {
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
		key := point.Metric["db_system"] + "|" + point.Metric["net_peer_name"]
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
