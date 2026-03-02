package traces

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"last9-mcp/internal/constants"
	"last9-mcp/internal/deeplink"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var promQLRegexSpecialChars = regexp.MustCompile(`[.\-+*?^$()|\[\]{}\\/]`)

type promInstantResponse []struct {
	Metric map[string]string `json:"metric"`
	Value  []any             `json:"value"`
}

type promRangeResponse []struct {
	Metric map[string]string `json:"metric"`
	Values [][]any           `json:"values"`
}

type exceptionAggregate struct {
	ExceptionType          string
	ServiceName            string
	SpanName               string
	SpanKind               string
	DeploymentEnvironment  string
	Count                  float64
	FirstSeenAtMillisecond int64
	LastSeenAtMillisecond  int64
}

// GetExceptionsArgs defines the input structure for getting exceptions
type GetExceptionsArgs struct {
	Limit                 float64 `json:"limit,omitempty" jsonschema:"Maximum number of exceptions to return (default: 20, range: 1-1000)"`
	LookbackMinutes       float64 `json:"lookback_minutes,omitempty" jsonschema:"Number of minutes to look back from current time (default: 60, range: 1-10080)"`
	StartTimeISO          string  `json:"start_time_iso,omitempty" jsonschema:"Start time in RFC3339/ISO8601 format (e.g. 2026-02-09T15:04:05Z)"`
	EndTimeISO            string  `json:"end_time_iso,omitempty" jsonschema:"End time in RFC3339/ISO8601 format (e.g. 2026-02-09T16:04:05Z)"`
	ServiceName           string  `json:"service_name,omitempty" jsonschema:"Filter exceptions by service name (e.g. api-service)"`
	SpanName              string  `json:"span_name,omitempty" jsonschema:"Filter exceptions by span name (e.g. user_service)"`
	DeploymentEnvironment string  `json:"deployment_environment,omitempty" jsonschema:"Filter exceptions by deployment environment from resource attributes (e.g. production, staging)"`
}

// NewGetExceptionsHandler creates a handler for getting exceptions
func NewGetExceptionsHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, GetExceptionsArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args GetExceptionsArgs) (*mcp.CallToolResult, any, error) {
		limit := 20
		if args.Limit != 0 {
			limit = int(args.Limit)
		}
		if limit <= 0 {
			limit = 20
		}
		if limit > 100 {
			limit = 100 // Maximum limit for trace queries
		}

		lookbackMinutes := 60
		if args.LookbackMinutes != 0 {
			lookbackMinutes = int(args.LookbackMinutes)
		}

		// Prepare arguments map for GetTimeRange function
		arguments := make(map[string]interface{})
		if args.StartTimeISO != "" {
			arguments["start_time_iso"] = args.StartTimeISO
		}
		if args.EndTimeISO != "" {
			arguments["end_time_iso"] = args.EndTimeISO
		}

		// Get time range using the common utility
		startTime, endTime, err := utils.GetTimeRange(arguments, lookbackMinutes)
		if err != nil {
			return nil, nil, err
		}

		// Convert start/end times to milliseconds
		startMs := startTime.UnixMilli()
		endMs := endTime.UnixMilli()

		durationMinutes := int(endTime.Sub(startTime).Minutes())
		if endTime.Sub(startTime)%time.Minute != 0 {
			durationMinutes++
		}
		if durationMinutes <= 0 {
			durationMinutes = 1
		}

		baseFilter := buildExceptionBaseFilter(args)
		exceptionsQuery := buildExceptionsPromQL(baseFilter, fmt.Sprintf("%dm", durationMinutes))

		// Frontend parity: exceptions list is fetched from prom_query_instant over trace_*_count.
		resp, err := utils.MakePromInstantAPIQuery(ctx, client, exceptionsQuery, endTime.Unix(), cfg)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to execute exceptions instant query: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return nil, nil, fmt.Errorf("exceptions instant query failed with status %d: %s", resp.StatusCode, string(body))
		}

		var instantSeries promInstantResponse
		if err := json.NewDecoder(resp.Body).Decode(&instantSeries); err != nil {
			return nil, nil, fmt.Errorf("failed to decode exceptions instant response: %w", err)
		}

		lastSeenMap := map[string]int64{}
		shouldFetchLastSeen := endTime.Sub(startTime) >= 5*time.Minute

		if shouldFetchLastSeen {
			lookbackSeconds := int64(endTime.Sub(startTime).Seconds())
			if lookbackSeconds > 300 {
				lookbackSeconds = 300
			}
			if lookbackSeconds <= 0 {
				lookbackSeconds = 300
			}

			// Frontend parity: use prom_query range query over the last few minutes
			// to find the latest non-zero exception datapoint per exception group.
			lastSeenQuery := buildExceptionsPromQL(baseFilter, "1m")
			lastSeenResp, err := makePromRangeAPIQueryWithStep(ctx, client, lastSeenQuery, endTime.Unix(), lookbackSeconds, 60, cfg)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to execute exceptions last-seen range query: %w", err)
			}
			defer lastSeenResp.Body.Close()

			if lastSeenResp.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(lastSeenResp.Body)
				return nil, nil, fmt.Errorf("exceptions last-seen query failed with status %d: %s", lastSeenResp.StatusCode, string(body))
			}

			var rangeSeries promRangeResponse
			if err := json.NewDecoder(lastSeenResp.Body).Decode(&rangeSeries); err != nil {
				return nil, nil, fmt.Errorf("failed to decode exceptions last-seen response: %w", err)
			}

			for _, series := range rangeSeries {
				key := buildExceptionKey(
					orDefault(series.Metric["exception_type"], "Unknown"),
					orDefault(series.Metric["service_name"], "Unknown"),
					orDefault(series.Metric["span_name"], "Unknown"),
					orDefault(series.Metric["span_kind"], "UNKNOWN"),
				)

				var lastSeenMs int64
				for i := len(series.Values) - 1; i >= 0; i-- {
					if len(series.Values[i]) < 2 {
						continue
					}
					if parsePromNumber(series.Values[i][1]) <= 0 {
						continue
					}
					tsSeconds := parsePromTimestampSeconds(series.Values[i][0])
					if tsSeconds > 0 {
						lastSeenMs = tsSeconds * 1000
					}
					break
				}
				if lastSeenMs > 0 {
					lastSeenMap[key] = lastSeenMs
				}
			}
		}

		aggregates := make([]exceptionAggregate, 0, len(instantSeries))
		for _, point := range instantSeries {
			exceptionType := orDefault(point.Metric["exception_type"], "Unknown")
			serviceName := orDefault(point.Metric["service_name"], "Unknown")
			spanName := orDefault(point.Metric["span_name"], "Unknown")
			spanKind := orDefault(point.Metric["span_kind"], "UNKNOWN")

			var count float64
			if len(point.Value) > 1 {
				count = parsePromNumber(point.Value[1])
			}

			key := buildExceptionKey(exceptionType, serviceName, spanName, spanKind)
			lastSeenMs := endMs
			if v, ok := lastSeenMap[key]; ok {
				lastSeenMs = v
			}

			deploymentEnvironment := point.Metric["env"]
			if deploymentEnvironment == "" {
				deploymentEnvironment = args.DeploymentEnvironment
			}

			aggregates = append(aggregates, exceptionAggregate{
				ExceptionType:          exceptionType,
				ServiceName:            serviceName,
				SpanName:               spanName,
				SpanKind:               spanKind,
				DeploymentEnvironment:  deploymentEnvironment,
				Count:                  count,
				FirstSeenAtMillisecond: startMs,
				LastSeenAtMillisecond:  lastSeenMs,
			})
		}

		sort.SliceStable(aggregates, func(i, j int) bool {
			if aggregates[i].Count == aggregates[j].Count {
				return aggregates[i].ExceptionType < aggregates[j].ExceptionType
			}
			return aggregates[i].Count > aggregates[j].Count
		})

		if len(aggregates) > limit {
			aggregates = aggregates[:limit]
		}

		exceptions := make([]map[string]interface{}, 0, len(aggregates))
		for _, exceptionData := range aggregates {
			lastSeen := time.UnixMilli(exceptionData.LastSeenAtMillisecond).UTC().Format(time.RFC3339)
			firstSeen := time.UnixMilli(exceptionData.FirstSeenAtMillisecond).UTC().Format(time.RFC3339)

			exceptions = append(exceptions, map[string]interface{}{
				"trace_id":               nil,
				"span_id":                nil,
				"service_name":           exceptionData.ServiceName,
				"span_name":              exceptionData.SpanName,
				"timestamp":              lastSeen,
				"exception_type":         exceptionData.ExceptionType,
				"exception_message":      "",
				"exception_stacktrace":   "",
				"exception_escaped":      nil,
				"deployment_environment": exceptionData.DeploymentEnvironment,
				"service_namespace":      "",
				"service_instance_id":    "",
				"span_kind":              exceptionData.SpanKind,
				"duration_ms":            nil,
				"status_code":            "",
				"count":                  exceptionData.Count,
				"first_seen":             firstSeen,
				"last_seen":              lastSeen,
			})
		}

		// Format response
		responseData := map[string]interface{}{
			"exceptions": exceptions,
			"count":      len(exceptions),
			"start_time": startTime.Format("2006-01-02T15:04:05Z"),
			"end_time":   endTime.Format("2006-01-02T15:04:05Z"),
		}

		jsonData, err := json.Marshal(responseData)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to marshal response: %w", err)
		}

		// Build deep link URL
		dlBuilder := deeplink.NewBuilder(cfg.OrgSlug, cfg.ClusterID)
		dashboardURL := dlBuilder.BuildExceptionsLink(startMs, endMs)

		return &mcp.CallToolResult{
			Meta: deeplink.ToMeta(dashboardURL),
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: string(jsonData),
				},
			},
		}, nil, nil
	}
}

func buildExceptionBaseFilter(args GetExceptionsArgs) string {
	matchers := make([]string, 0, 3)

	if args.ServiceName != "" {
		matchers = append(matchers, fmt.Sprintf("service_name=~'%s'", escapePromQLLabelValue(args.ServiceName)))
	}

	if args.SpanName != "" {
		matchers = append(matchers, fmt.Sprintf("span_name=~'%s'", escapePromQLLabelValue(args.SpanName)))
	}

	if args.DeploymentEnvironment != "" {
		matchers = append(matchers, fmt.Sprintf("env=~'%s'", escapePromQLLabelValue(args.DeploymentEnvironment)))
	}

	return strings.Join(matchers, ", ")
}

func buildExceptionsPromQL(baseFilter string, rangeSelector string) string {
	selector := "exception_type!=''"
	if baseFilter != "" {
		selector = fmt.Sprintf("%s, exception_type!=''", baseFilter)
	}

	return fmt.Sprintf(`
		sum by (exception_type, service_name, span_name, span_kind, env) (
			sum_over_time(trace_endpoint_count{%s}[%s])
		) or
		sum by (exception_type, service_name, span_name, span_kind, env) (
			sum_over_time(trace_client_count{%s}[%s])
		) or
		sum by (exception_type, service_name, span_name, span_kind, env) (
			sum_over_time(trace_internal_count{%s}[%s])
		)
	`, selector, rangeSelector, selector, rangeSelector, selector, rangeSelector)
}

func buildExceptionKey(exceptionType, serviceName, spanName, spanKind string) string {
	return strings.Join([]string{exceptionType, serviceName, spanName, spanKind}, ":")
}

func parsePromNumber(raw any) float64 {
	switch value := raw.(type) {
	case float64:
		return value
	case float32:
		return float64(value)
	case int:
		return float64(value)
	case int64:
		return float64(value)
	case json.Number:
		f, err := value.Float64()
		if err == nil {
			return f
		}
	case string:
		f, err := strconv.ParseFloat(value, 64)
		if err == nil {
			return f
		}
	}

	return 0
}

func parsePromTimestampSeconds(raw any) int64 {
	switch value := raw.(type) {
	case int64:
		return value
	case int:
		return int64(value)
	case float64:
		return int64(value)
	case float32:
		return int64(value)
	case json.Number:
		f, err := value.Float64()
		if err == nil {
			return int64(f)
		}
	case string:
		f, err := strconv.ParseFloat(value, 64)
		if err == nil {
			return int64(f)
		}
	}

	return 0
}

func orDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func escapePromQLLabelValue(value string) string {
	if value == "" {
		return value
	}

	escapedSingleQuotes := strings.ReplaceAll(value, "'", `\'`)

	return promQLRegexSpecialChars.ReplaceAllStringFunc(escapedSingleQuotes, func(match string) string {
		return `\` + match
	})
}

func makePromRangeAPIQueryWithStep(
	ctx context.Context,
	client *http.Client,
	promql string,
	timestamp int64,
	window int64,
	step int,
	cfg models.Config,
) (*http.Response, error) {
	params := struct {
		Query     string `json:"query"`
		Timestamp int64  `json:"timestamp"`
		Window    int64  `json:"window"`
		Step      int    `json:"step,omitempty"`
		ReadURL   string `json:"read_url"`
		Username  string `json:"username"`
		Password  string `json:"password"`
	}{
		Query:     promql,
		Timestamp: timestamp,
		Window:    window,
		Step:      step,
		ReadURL:   cfg.PrometheusReadURL,
		Username:  cfg.PrometheusUsername,
		Password:  cfg.PrometheusPassword,
	}

	bodyBytes, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}

	reqURL := fmt.Sprintf("%s%s", cfg.APIBaseURL, constants.EndpointPromQuery)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}

	httpReq.Header.Set(constants.HeaderContentType, constants.HeaderContentTypeJSON)
	httpReq.Header.Set(constants.HeaderXLast9APIToken, constants.BearerPrefix+cfg.TokenManager.GetAccessToken(ctx))

	return client.Do(httpReq)
}
