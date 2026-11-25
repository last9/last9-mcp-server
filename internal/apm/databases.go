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
