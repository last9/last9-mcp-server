package apm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"sort"
	"strconv"
	"sync"

	"last9-mcp/internal/deeplink"
	"last9-mcp/internal/models"
	"last9-mcp/internal/telemetry/traces"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// --- get_database_slow_queries tool ---

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
	Source      string  `json:"source"` // "trace" or "log"
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

		pipeline, err := buildDatabaseSlowQueryTracePipeline(args)
		if err != nil {
			return nil, nil, err
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
				var transportErr *utils.HTTPTransportError
				if errors.As(err, &transportErr) {
					traceErr = errors.New("failed to query slow database traces: trace service could not be reached")
					return
				}
				traceErr = fmt.Errorf("failed to query slow database traces: %w", err)
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				_, _ = io.CopyN(io.Discard, resp.Body, 4<<10)
				traceErr = fmt.Errorf("traces API returned status %d", resp.StatusCode)
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
					&mcp.TextContent{Text: apmEmptyAdvice(
						"slow database queries",
						apmGuidanceArgs{
							serviceName: args.ServiceName,
							env:         args.Env,
							dbSystem:    args.DBSystem,
							startUnix:   startTime,
							endUnix:     endTime,
						},
						"verify database client spans or slow-query logs are instrumented",
						"lower min_duration_ms if it was supplied",
					)},
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

type GetDatabaseQueriesArgs struct {
	DBSystem        string  `json:"db_system" jsonschema:"(Required) Database system (e.g. postgresql, mysql, mongodb, redis)"`
	Host            string  `json:"host,omitempty" jsonschema:"Database host filter (net_peer_name)"`
	Env             string  `json:"env,omitempty" jsonschema:"(Optional) Exact deployment environment name, e.g. production. Default all environments (empty or .*). Anchored forms like ^production$ are accepted and matched exactly. Alternation (a|b) and wildcards (prod.*) are rejected; list names with get_service_environments."`
	LookbackMinutes float64 `json:"lookback_minutes,omitempty" jsonschema:"Minutes to look back (default: 60)"`
	StartTimeISO    string  `json:"start_time_iso,omitempty" jsonschema:"Start time in RFC3339 format"`
	EndTimeISO      string  `json:"end_time_iso,omitempty" jsonschema:"End time in RFC3339 format"`
	SortBy          string  `json:"sort_by,omitempty" jsonschema:"Sort by: throughput (default), latency, or errors"`
}

type QueryPattern struct {
	SpanName    string  `json:"span_name"`
	CallsPerMin float64 `json:"calls_per_min"`
	AvgLatency  float64 `json:"avg_latency_ms"`
	P95Latency  float64 `json:"p95_latency_ms"`
	ErrorRate   float64 `json:"error_rate_pct"`
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

		baseFilter, envDisplay, err := buildDBBaseFilter(args.DBSystem, args.Host, args.Env)
		if err != nil {
			return nil, nil, err
		}

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
					&mcp.TextContent{Text: apmEmptyAdvice(
						"query patterns",
						apmGuidanceArgs{env: envLiteral(envDisplay), dbSystem: args.DBSystem, startUnix: startTime, endUnix: endTime},
						"ensure services are instrumented with OpenTelemetry database client spans",
						"verify host if one was supplied",
					)},
				},
			}, nil, nil
		}

		// 2. Remaining metrics in parallel (avg latency, p95 latency, error count, total count)
		avgLatencyQuery := fmt.Sprintf(
			`avg by(span_name)(avg_over_time(trace_client_duration{%s, quantile="p50"}[%dm]))`,
			baseFilter, durationMin,
		)
		p95LatencyQuery := fmt.Sprintf(
			`avg by(span_name)(avg_over_time(trace_client_duration{%s, quantile="p95"}[%dm]))`,
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
		response["env"] = envDisplay
		if requested := envRequested(args.Env, envDisplay); requested != "" {
			response["env_requested"] = requested
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

func buildDBBaseFilter(dbSystem, host, env string) (filter, envDisplay string, err error) {
	envDisplay = ".*"
	filter = fmt.Sprintf(
		`span_kind=~"SPAN_KIND_CLIENT|SPAN_KIND_INTERNAL", db_system="%s"`,
		utils.EscapePromQLLabel(dbSystem),
	)
	if host != "" {
		filter += fmt.Sprintf(`, net_peer_name="%s"`, utils.EscapePromQLLabel(host))
	}
	if env != "" {
		var envPattern string
		envPattern, _, envDisplay, err = literalEnvPattern(env)
		if err != nil {
			return "", "", err
		}
		filter += fmt.Sprintf(`, env=~"%s"`, utils.EscapePromQLLabel(envPattern))
	}
	return filter, envDisplay, nil
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

// --- helpers ---

func buildDatabaseSlowQueryTracePipeline(args GetDatabaseSlowQueriesArgs) ([]map[string]any, error) {
	var conditions []any

	conditions = append(conditions, map[string]any{
		"$regex": []any{"SpanKind", "SPAN_KIND_CLIENT|SPAN_KIND_INTERNAL"},
	})

	if args.DBSystem != "" {
		conditions = append(conditions, map[string]any{
			"$eq": []any{traces.SpanAttributeField("db.system"), args.DBSystem},
		})
	} else {
		conditions = append(conditions, map[string]any{
			"$neq": []any{traces.SpanAttributeField("db.system"), ""},
		})
	}

	if args.Host != "" {
		conditions = append(conditions, map[string]any{
			"$eq": []any{traces.SpanAttributeField("net.peer.name"), args.Host},
		})
	}

	if args.ServiceName != "" {
		conditions = append(conditions, map[string]any{
			"$eq": []any{"ServiceName", args.ServiceName},
		})
	}

	if args.Env != "" {
		conditions = append(conditions, map[string]any{
			"$eq": []any{traces.ResourceAttributeField("deployment.environment"), args.Env},
		})
	}

	if args.MinDurationMs > 0 {
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

	if err := traces.SanitizeTraceJSONQuery(pipeline); err != nil {
		return nil, fmt.Errorf("invalid slow query trace pipeline: %w", err)
	}

	return pipeline, nil
}

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

	// Apply MinDurationMs client-side, matching the trace-side $gte filter.
	// The duration lives inside the JSON log body (under instrumentation-specific
	// keys), so it cannot be pushed into the server-side pipeline. Note the
	// trade-off: the API returns at most `limit` entries *before* this filter,
	// so qualifying slow logs beyond that page may be missed. Acceptable since
	// this path is best-effort (traces are the primary source).
	return filterSlowQueriesByMinDuration(extractSlowQueryLogs(rawResult), args.MinDurationMs)
}

// filterSlowQueriesByMinDuration drops queries below minDurationMs.
// A non-positive threshold means "unset" and returns the slice unchanged.
func filterSlowQueriesByMinDuration(queries []SlowQuery, minDurationMs float64) []SlowQuery {
	if minDurationMs <= 0 {
		return queries
	}
	return slices.DeleteFunc(queries, func(q SlowQuery) bool {
		return q.DurationMs < minDurationMs
	})
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
