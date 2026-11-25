package apm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"

	"last9-mcp/internal/last9api"
	"last9-mcp/internal/last9api/prom"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/sync/errgroup"
)

type GetDatabaseQueriesArgs struct {
	DBSystem        string  `json:"db_system" jsonschema:"(Required) Database system (e.g. postgresql, mysql, mongodb, redis)"`
	ServiceName     string  `json:"service_name,omitempty" jsonschema:"Exact calling service name. Omit to include all calling services."`
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

		baseFilter, envDisplay, err := buildDBBaseFilter(args)
		if err != nil {
			return nil, nil, err
		}

		// 1. Throughput first (to check if any patterns exist)
		api := last9api.NewClient(client, cfg)
		datasource := prom.Datasource{ReadURL: cfg.PrometheusReadURL, Username: cfg.PrometheusUsername, Password: cfg.PrometheusPassword}
		queryValues := func(query string) (map[string]float64, error) {
			series, err := prom.QueryInstant(ctx, api, prom.QueryInstantInput{Datasource: datasource, Query: query, Timestamp: endTime})
			if err != nil {
				return nil, err
			}
			values := make(map[string]float64, len(series))
			for _, point := range series {
				if span := point.Metric["span_name"]; span != "" {
					values[span] = parsePromValue(point.Value)
				}
			}
			return values, nil
		}
		throughputQuery := fmt.Sprintf(
			`sum by(span_name)(sum_over_time(trace_client_count{%s}[%dm])) / %d`,
			baseFilter, durationMin, durationMin,
		)
		throughput, err := queryValues(throughputQuery)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to fetch throughput: %w", err)
		}

		if len(throughput) == 0 {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					&mcp.TextContent{Text: apmEmptyAdvice(
						"query patterns",
						apmGuidanceArgs{serviceName: args.ServiceName, env: envLiteral(envDisplay), dbSystem: args.DBSystem, startUnix: startTime, endUnix: endTime},
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
			avgLatency, p95Latency, errorCounts, totalCounts map[string]float64
			group                                            errgroup.Group
		)
		group.Go(func() error {
			var err error
			avgLatency, err = queryValues(avgLatencyQuery)
			return metricQueryError("average latency", err)
		})
		group.Go(func() error {
			var err error
			p95Latency, err = queryValues(p95LatencyQuery)
			return metricQueryError("p95 latency", err)
		})
		group.Go(func() error {
			var err error
			errorCounts, err = queryValues(errorCountQuery)
			return metricQueryError("error count", err)
		})
		group.Go(func() error {
			var err error
			totalCounts, err = queryValues(totalCountQuery)
			return metricQueryError("total count", err)
		})
		if err := group.Wait(); err != nil {
			return nil, nil, err
		}

		result := make([]QueryPattern, 0, len(throughput))
		for span, rpm := range throughput {
			row := QueryPattern{SpanName: span, CallsPerMin: rpm, AvgLatency: avgLatency[span], P95Latency: p95Latency[span]}
			if total := totalCounts[span]; total > 0 {
				row.ErrorRate = (errorCounts[span] / total) * 100
			}
			result = append(result, row)
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
		if args.ServiceName != "" {
			response["service_name"] = args.ServiceName
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

func metricQueryError(metric string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("failed to fetch %s: %w", metric, err)
}

func buildDBBaseFilter(args GetDatabaseQueriesArgs) (filter, envDisplay string, err error) {
	envDisplay = ".*"
	filter = fmt.Sprintf(
		`span_kind=~"SPAN_KIND_CLIENT|SPAN_KIND_INTERNAL", db_system="%s"`,
		utils.EscapePromQLLabel(args.DBSystem),
	)
	if args.Host != "" {
		filter += fmt.Sprintf(`, net_peer_name="%s"`, utils.EscapePromQLLabel(args.Host))
	}
	if args.ServiceName != "" {
		filter += fmt.Sprintf(`, service_name="%s"`, utils.EscapePromQLLabel(args.ServiceName))
	}
	if args.Env != "" {
		var envPattern string
		envPattern, _, envDisplay, err = literalEnvPattern(args.Env)
		if err != nil {
			return "", "", err
		}
		filter += fmt.Sprintf(`, env=~"%s"`, utils.EscapePromQLLabel(envPattern))
	}
	return filter, envDisplay, nil
}
