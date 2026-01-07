package apm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type ServiceSummary struct {
	Throughput, ErrorRate, ResponseTime float64
	ServiceName, Env                    string
}

type apiPromInstantResp []struct {
	Metric map[string]string `json:"metric"`
	Value  []any             `json:"value"`
}

type apiPromRangeResp []struct {
	Metric map[string]string `json:"metric"`
	Values [][]any           `json:"values"`
}

// Input structs for MCP SDK handlers
type ServiceSummaryArgs struct {
	StartTimeISO string `json:"start_time_iso,omitempty" jsonschema:"Start time in ISO8601 format (e.g. 2024-06-01T12:00:00Z)"`
	EndTimeISO   string `json:"end_time_iso,omitempty" jsonschema:"End time in ISO8601 format (e.g. 2024-06-01T13:00:00Z)"`
	Env          string `json:"env,omitempty" jsonschema:"Environment to filter by (default: .*, e.g. prod)"`
}

type ServiceEnvironmentsArgs struct {
	StartTimeISO string `json:"start_time_iso,omitempty" jsonschema:"Start time in ISO8601 format (e.g. 2024-06-01T12:00:00Z)"`
	EndTimeISO   string `json:"end_time_iso,omitempty" jsonschema:"End time in ISO8601 format (e.g. 2024-06-01T13:00:00Z)"`
}

type ServicePerformanceDetailsArgs struct {
	ServiceName  string `json:"service_name" jsonschema:"Name of the service to get performance details for (required)"`
	StartTimeISO string `json:"start_time_iso,omitempty" jsonschema:"Start time in ISO8601 format (e.g. 2024-06-01T12:00:00Z)"`
	EndTimeISO   string `json:"end_time_iso,omitempty" jsonschema:"End time in ISO8601 format (e.g. 2024-06-01T13:00:00Z)"`
	Env          string `json:"env,omitempty" jsonschema:"Environment to filter by (default: .*, e.g. prod)"`
}

type ServiceOperationsSummaryArgs struct {
	ServiceName  string `json:"service_name" jsonschema:"Name of the service to get operations summary for (required)"`
	StartTimeISO string `json:"start_time_iso,omitempty" jsonschema:"Start time in ISO8601 format (e.g. 2024-06-01T12:00:00Z)"`
	EndTimeISO   string `json:"end_time_iso,omitempty" jsonschema:"End time in ISO8601 format (e.g. 2024-06-01T13:00:00Z)"`
	Env          string `json:"env,omitempty" jsonschema:"Environment to filter by (default: .*, e.g. prod)"`
}

type ServiceDependencyGraphArgs struct {
	StartTimeISO string `json:"start_time_iso,omitempty" jsonschema:"Start time in ISO8601 format (e.g. 2024-06-01T12:00:00Z)"`
	EndTimeISO   string `json:"end_time_iso,omitempty" jsonschema:"End time in ISO8601 format (e.g. 2024-06-01T13:00:00Z)"`
	Env          string `json:"env,omitempty" jsonschema:"Environment to filter by (default: .*, e.g. prod)"`
	ServiceName  string `json:"service_name,omitempty" jsonschema:"Service name to focus on in the dependency graph (e.g. api-service)"`
}

type PromqlRangeQueryArgs struct {
	Query        string `json:"query" jsonschema:"PromQL query to execute (required)"`
	StartTimeISO string `json:"start_time_iso,omitempty" jsonschema:"Start time in ISO8601 format (e.g. 2024-06-01T12:00:00Z)"`
	EndTimeISO   string `json:"end_time_iso,omitempty" jsonschema:"End time in ISO8601 format (e.g. 2024-06-01T13:00:00Z)"`
}

type PromqlInstantQueryArgs struct {
	Query   string `json:"query" jsonschema:"PromQL query to execute (required)"`
	TimeISO string `json:"time_iso,omitempty" jsonschema:"Evaluation time in ISO8601 format (e.g. 2024-06-01T12:00:00Z)"`
}

type PromqlLabelValuesArgs struct {
	MatchQuery   string `json:"match_query,omitempty" jsonschema:"PromQL query to match series (e.g. up{job=\"prometheus\"})"`
	Label        string `json:"label" jsonschema:"Label name to get values for (required)"`
	StartTimeISO string `json:"start_time_iso,omitempty" jsonschema:"Start time in ISO8601 format (e.g. 2024-06-01T12:00:00Z)"`
	EndTimeISO   string `json:"end_time_iso,omitempty" jsonschema:"End time in ISO8601 format (e.g. 2024-06-01T13:00:00Z)"`
}

type PromqlLabelsArgs struct {
	MatchQuery   string `json:"match_query,omitempty" jsonschema:"PromQL query to match series (e.g. up{job=\"prometheus\"})"`
	StartTimeISO string `json:"start_time_iso,omitempty" jsonschema:"Start time in ISO8601 format (e.g. 2024-06-01T12:00:00Z)"`
	EndTimeISO   string `json:"end_time_iso,omitempty" jsonschema:"End time in ISO8601 format (e.g. 2024-06-01T13:00:00Z)"`
}

const GetServiceSummaryDescription = `
	Get service summary over a given time range.
	Includes service name, environment, throughput, error rate, and response time.
	All values are p95 quantiles over the time range.
	Response times are in milliseconds. Throughput and error rates are in requests per minute (rpm).
	Each service includes:
	- service name
	- environment
	- throughput in requests per minute (rpm)
	- error rate in requests per minute (rpm)
	- p95 response time in milliseconds
	Parameters:
	- start_time: (Required) Start time of the time range in ISO format.
	- end_time: (Required) End time of the time range in ISO format.
	- env: (Required) Environment to filter by. If not provided, defaults to all environments.
`

func NewServiceSummaryHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, ServiceSummaryArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args ServiceSummaryArgs) (*mcp.CallToolResult, any, error) {
		// Accept time range parameters
		var (
			startTimeParam, endTimeParam int64
		)
		// Accept end_time in ISO8601 format (e.g., "2024-06-01T13:00:00Z")
		if args.EndTimeISO != "" {
			t, err := time.Parse(time.RFC3339, args.EndTimeISO)
			if err != nil {
				return nil, nil, fmt.Errorf("invalid end_time_iso format, must be ISO8601: %w", err)
			}
			endTimeParam = t.Unix()
		} else {
			// Default end_time to current time
			endTimeParam = time.Now().Unix()
		}
		// Accept start_time in ISO8601 format (e.g., "2024-06-01T12:00:00Z")
		if args.StartTimeISO != "" {
			t, err := time.Parse(time.RFC3339, args.StartTimeISO)
			if err != nil {
				return nil, nil, fmt.Errorf("invalid start_time_iso format, must be ISO8601: %w", err)
			}
			startTimeParam = t.Unix()
		} else {
			// Default start_time to end_time - 1 hour
			startTimeParam = endTimeParam - 3600
		}

		// Accept env from parameters if provided
		env := args.Env
		if env == "" {
			env = ".*" // default value
		}
		// get the value of service througputs using the query
		// quantile_over_time(0.95, sum by (service_name)(trace_endpoint_count{service_name=~'.*', env=~'prod', span_kind=~'SPAN_KIND_SERVER|SPAN_KIND_CLIENT'})[30m])
		// add the filter values in the promql from the filterParams
		// Build PromQL filter string from filterParams
		// Build PromQL query
		promql := fmt.Sprintf(
			"quantile_over_time(0.95, sum by (service_name)(trace_endpoint_count{env=~'%s', span_kind='SPAN_KIND_SERVER'}[%dm]))",
			env,
			int((endTimeParam-startTimeParam)/60),
		)

		// Prepare request to Prometheus (or your metrics backend)
		httpResp, err := utils.MakePromInstantAPIQuery(ctx, client, promql, endTimeParam, cfg)
		if err != nil {
			return nil, nil, err
		}
		defer httpResp.Body.Close()

		var promResp map[string]ServiceSummary
		if httpResp.StatusCode != http.StatusOK {
			return nil, nil, fmt.Errorf("failed to get service summary: %s", httpResp.Status)
		}

		// Extract service summary map from PromQL response
		var thrResp apiPromInstantResp
		if err := json.NewDecoder(httpResp.Body).Decode(&thrResp); err != nil {
			return nil, nil, err
		}

		promResp = make(map[string]ServiceSummary)
		for _, r := range thrResp {
			serviceName := r.Metric["service_name"]

			valStr, _ := r.Value[1].(string)
			val, _ := strconv.ParseFloat(valStr, 64)

			promResp[serviceName] = ServiceSummary{
				ServiceName:  serviceName,
				Env:          env,
				Throughput:   val,
				ErrorRate:    0, // Placeholder, set if available
				ResponseTime: 0, // Placeholder, set if available
			}
		}
		// If no services found, return empty result
		if len(promResp) == 0 {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					&mcp.TextContent{
						Text: "No services found for the given parameters",
					},
				},
			}, nil, nil
		}
		// Make another prom_query_instant call for response time
		respTimePromql := fmt.Sprintf(
			"quantile_over_time(0.95, sum by (service_name)(trace_service_response_time{quantile=\"p95\", env=~'%s'}[%dm]))",
			env,
			int((endTimeParam-startTimeParam)/60),
		)
		// Prepare request to Prometheus (or your metrics backend)
		httpResp, err = utils.MakePromInstantAPIQuery(ctx, client, respTimePromql, endTimeParam, cfg)
		if err != nil {
			return nil, nil, err
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode != http.StatusOK {
			return nil, nil, fmt.Errorf("failed to get service summary: %s", httpResp.Status)
		}

		var respTimeRaw apiPromInstantResp
		if err := json.NewDecoder(httpResp.Body).Decode(&respTimeRaw); err != nil {
			return nil, nil, err
		}

		for _, r := range respTimeRaw {
			serviceName := r.Metric["service_name"]
			valStr, _ := r.Value[1].(string)
			val, _ := strconv.ParseFloat(valStr, 64)
			if summary, ok := promResp[serviceName]; ok {
				summary.ResponseTime = val
				promResp[serviceName] = summary
			} else {
				promResp[serviceName] = ServiceSummary{
					ServiceName:  serviceName,
					Env:          env,
					Throughput:   0,
					ErrorRate:    0,
					ResponseTime: val,
				}
			}
		}
		// Make another prom_query_instant call for error rate
		errorRateQuery := fmt.Sprintf(
			"quantile_over_time(0.95, sum by (service_name)(trace_endpoint_count{env=~'%s', span_kind=~'SPAN_KIND_SERVER', http_status_code=~\"5.*\"}[%dm]))",
			env,
			int((endTimeParam-startTimeParam)/60),
		)
		// Prepare request to Prometheus (or your metrics backend)
		httpResp, err = utils.MakePromInstantAPIQuery(ctx, client, errorRateQuery, endTimeParam, cfg)
		if err != nil {
			return nil, nil, err
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode != http.StatusOK {
			return nil, nil, fmt.Errorf("failed to get service summary: %s", httpResp.Status)
		}

		var errRateRaw apiPromInstantResp
		if err := json.NewDecoder(httpResp.Body).Decode(&errRateRaw); err != nil {
			return nil, nil, err
		}

		for _, r := range errRateRaw {
			serviceName := r.Metric["service_name"]
			valStr, _ := r.Value[1].(string)
			val, _ := strconv.ParseFloat(valStr, 64)
			if summary, ok := promResp[serviceName]; ok {
				summary.ErrorRate = val
				promResp[serviceName] = summary
			} else {
				promResp[serviceName] = ServiceSummary{
					ServiceName:  serviceName,
					Env:          env,
					Throughput:   0,
					ErrorRate:    val,
					ResponseTime: 0,
				}
			}
		}
		returnText, err := json.Marshal(promResp)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to marshal response: %w", err)
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: string(returnText),
				},
			},
		}, nil, nil
	}
}

const GetServicePerformanceDetails = `
	Get service performance metrics over a given time range.
	Returns the following information
		- service name
		- environment
		- throughput in rpm
		- error rate in rpm for 4xx and 5xx errors
		- error percentage
		- p50, p90, p95 and avg response times in seconds
		- apdex score
		- availability in percentage
		- top 10 web operations by response time
		- top 10 operations by error rate
		- top 10 errors or exceptions by count for the service
	The details for the operations in the "top 10 web operations by response time" and "top 10 operations by error rate" can be fetched using the "get_service_operation_details" tool.
	This tool can be used to get all perforamnce and debugging details for a service over a time range.
	It can also be used to get a summary for performance bottlenecks and errors / exceptions in a service.
	Some fields are in the promql resonse format. Sample response:
	[{"metric":{"service_name":"svc1","env":"prod"},"values":[[1700000000,"0.5"]]},{"metric":{"service_name":"svc2","env":"prod"},"values":[[1700000001,"0.1"]]}]
	where the "metric" key is a dict of metadata, the first value in "values" is the timestamp in seconds and the second value is the value of the metric.
	The fields in the response are:
	- service_name: Name of the service.
	- env: Environment of the service.
	- throughput: Throughput in requests per minute (rpm) by status code. The format of this is in promql response format.
	- error_rate: Error rate in requests per minute (rpm) by status code. The format of this is in promql response format.
	- error_percentage: Error percentage in requests by status code. The format of this is in promql response format.
	- response_times: Response times in seconds by quantile (p50, p90, p95, avg). The format of this is in promql response format.
	- apdex_score: Apdex score over the time range. The format of this is in promql response format.
	- availability: Availability in percentage over the time range. The format of this is in promql response format.
	- top_operations: Top operations by response time and error rate. The format of this is a dict of operations and their throuputs
	- top_errors: Top errors or exceptions by count. The format of this is a dict of errors and their counts.
	- top_operations.by_response_time: Top 10 operations by response time. The format of this is a list of dicts with operation name and response time.
	- top_operations.by_error_rate: Top 10 operations by error rate. The format of this is a list of dicts with operation name and error count.
	- top_errors: Top 10 errors or exceptions by count. The format of this is a list of dicts with exception type (or http error code) and count. 
	Parameters:
	- start_time: (Required) Start time of the time range in ISO format.
	- end_time: (Required) End time of the time range in ISO format.
	- env: (Required) Environment to filter by. Use "get_service_environments" tool to get available environments.
`

type TimeSeriesPoint struct {
	Timestamp uint64  `json:"timestamp"`
	Value     float64 `json:"value"`
}

type TimeSeries struct {
	Metric map[string]string `json:"metric"`
	Values []TimeSeriesPoint `json:"values"`
}

type PromRangeResponse struct {
	Metric map[string]string `json:"metric"`
	Values [][]any           `json:"values"`
}

func parsePromTimeSeries(respBody []byte) ([]TimeSeries, error) {
	var promResp []PromRangeResponse
	var resp []TimeSeries
	if err := json.Unmarshal(respBody, &promResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal Prometheus response: %w", err)
	}
	// Convert Prometheus response to TimeSeries format
	for _, r := range promResp {
		series := TimeSeries{
			Metric: r.Metric,
			Values: make([]TimeSeriesPoint, 0, len(r.Values)),
		}
		for _, v := range r.Values {
			if len(v) != 2 {
				return nil, fmt.Errorf("invalid value format in Prometheus response: %v", v)
			}
			if ts, ok := v[0].(float64); ok {
				if valStr, ok := v[1].(string); ok {
					val, err := strconv.ParseFloat(valStr, 64)
					if err != nil {
						return nil, fmt.Errorf("failed to parse value: %w", err)
					}
					point := TimeSeriesPoint{
						Timestamp: uint64(ts),
						Value:     val,
					}
					series.Values = append(series.Values, point)
				} else {
					return nil, fmt.Errorf("invalid value type in Prometheus response: %T", v[1])
				}
			} else {
				return nil, fmt.Errorf("invalid timestamp type in Prometheus response: %T", v[0])
			}
		}
		resp = append(resp, series)
	}
	return resp, nil
}

type ServiceOperationsSummaryResponse struct {
	ServiceName string                    `json:"service_name"`
	Env         string                    `json:"env"`
	Operations  []ServiceOperationSummary `json:"operations"`
}

type ServiceOperationSummary struct {
	Name            string             `json:"name"`
	ServiceName     string             `json:"service_name"`
	Env             string             `json:"env"`
	DBSystem        string             `json:"db_system,omitempty"`
	MessagingSystem string             `json:"messaging_system,omitempty"`
	NetPeerName     string             `json:"net_peer_name,omitempty"`
	RPCSystem       string             `json:"rpc_system,omitempty"`
	Throughput      float64            `json:"throughput"`
	ErrorRate       float64            `json:"error_rate"`
	ResponseTime    map[string]float64 `json:"response_time"`
	ErrorPercent    float64            `json:"error_percent"`
}

type ServicePerformanceDetails struct {
	ServiceName   string       `json:"service_name"`
	Env           string       `json:"env"`
	Throughput    []TimeSeries `json:"throughput"` // by status code
	ErrorRate     []TimeSeries `json:"error_rate"` // by status code
	ErrorPercent  []TimeSeries `json:"error_percentage"`
	ResponseTimes []TimeSeries `json:"response_times"` // p50, p90, p95, avg
	ApdexScore    []TimeSeries `json:"apdex_score"`
	Availability  []TimeSeries `json:"availability"`
	TopOperations struct {
		ByResponseTime []map[string]float64 `json:"by_response_time"`
		ByErrorRate    []map[string]int64   `json:"by_error_rate"`
	} `json:"top_operations"`
	TopErrors []map[string]int64 `json:"top_errors"`
}

func NewServicePerformanceDetailsHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, ServicePerformanceDetailsArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args ServicePerformanceDetailsArgs) (*mcp.CallToolResult, any, error) {
		// Parse time parameters
		var (
			startTimeParam, endTimeParam int64
		)

		// Handle end_time
		if args.EndTimeISO != "" {
			t, err := time.Parse(time.RFC3339, args.EndTimeISO)
			if err != nil {
				return nil, nil, fmt.Errorf("invalid end_time_iso format, must be ISO8601: %w", err)
			}
			endTimeParam = t.Unix()
		} else {
			endTimeParam = time.Now().Unix()
		}

		// Handle start_time
		if args.StartTimeISO != "" {
			t, err := time.Parse(time.RFC3339, args.StartTimeISO)
			if err != nil {
				return nil, nil, fmt.Errorf("invalid start_time_iso format, must be ISO8601: %w", err)
			}
			startTimeParam = t.Unix()
		} else {
			startTimeParam = endTimeParam - 3600
		}

		// Handle environment
		env := args.Env
		if env == "" {
			env = ".*"
		}

		// Handle service_name
		serviceName := args.ServiceName
		if serviceName == "" {
			return nil, nil, fmt.Errorf("service_name is required")
		}

		timeRange := fmt.Sprintf("%dm", int((endTimeParam-startTimeParam)/60))

		details := ServicePerformanceDetails{
			ServiceName: serviceName,
			Env:         env,
		}

		// Get Apdex Score over time range as a vector
		apdexQuery := fmt.Sprintf(
			"sum(trace_service_apdex_score{service_name='%s', env=~'%s'})",
			serviceName, env,
		)
		httpResp, err := utils.MakePromRangeAPIQuery(ctx, client, apdexQuery, startTimeParam, endTimeParam, cfg)
		if err != nil {
			return nil, nil, err
		}
		defer httpResp.Body.Close()

		if httpResp.StatusCode == http.StatusOK {
			data, err := io.ReadAll(httpResp.Body)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to read response body: %w", err)
			}
			seriesList, err := parsePromTimeSeries(data)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to parse apdex score: %w", err)
			}
			details.ApdexScore = seriesList
		}

		// Get Response Times - keep vector output
		rtQuery := fmt.Sprintf(
			"sum by (quantile) (trace_service_response_time{service_name='%s', env='%s'}[%s])",
			serviceName, env, timeRange,
		)
		httpResp, err = utils.MakePromRangeAPIQuery(ctx, client, rtQuery, startTimeParam, endTimeParam, cfg)
		if err != nil {
			return nil, nil, err
		}
		defer httpResp.Body.Close()

		if httpResp.StatusCode == http.StatusOK {
			data, err := io.ReadAll(httpResp.Body)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to read response body: %w", err)
			}
			seriesList, err := parsePromTimeSeries(data)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to parse response times: %w", err)
			}
			details.ResponseTimes = seriesList
		}

		// Get Availability over time range as a vector
		availQuery := fmt.Sprintf(
			"(1 - (sum(rate(trace_endpoint_count{service_name='%s', env='%s', span_kind='SPAN_KIND_SERVER', http_status_code=~'4.*|5.*'}[%s])) or 0) / (sum(rate(trace_endpoint_count{service_name='%s', env='%s', span_kind='SPAN_KIND_SERVER'}[%s])) + 0.0000001)) * 100 default -999",
			serviceName, env, timeRange, serviceName, env, timeRange,
		)
		httpResp, err = utils.MakePromRangeAPIQuery(ctx, client, availQuery, startTimeParam, endTimeParam, cfg)
		if err != nil {
			return nil, nil, err
		}
		defer httpResp.Body.Close()

		if httpResp.StatusCode == http.StatusOK {
			data, err := io.ReadAll(httpResp.Body)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to read response body: %w", err)
			}
			availabilitySeries, err := parsePromTimeSeries(data)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to parse availability response: %w", err)
			}
			details.Availability = availabilitySeries
		}

		// Get Throughput by status code - keep vector output
		throughputQuery := fmt.Sprintf(
			"sum by (http_status_code)(rate(trace_endpoint_count{service_name='%s', env='%s', span_kind='SPAN_KIND_SERVER'}[%s])) * 60 default 0",
			serviceName, env, timeRange,
		)
		httpResp, err = utils.MakePromRangeAPIQuery(ctx, client, throughputQuery, startTimeParam, endTimeParam, cfg)
		if err != nil {
			return nil, nil, err
		}
		defer httpResp.Body.Close()

		if httpResp.StatusCode == http.StatusOK {
			// read response body to byte array
			data, err := io.ReadAll(httpResp.Body)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to read response body: %w", err)
			}
			details.Throughput, err = parsePromTimeSeries(data)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to parse throughput response: %w", err)
			}

		}

		// Get Error Rate by status code - keep vector output
		errorRateQuery := fmt.Sprintf(
			"sum by (service_name, http_status_code)(rate(trace_endpoint_count{service_name='%s', env='%s', span_kind='SPAN_KIND_SERVER', http_status_code=~'4.*|5.*'}[%s])) * 60 default 0",
			serviceName, env, timeRange,
		)
		httpResp, err = utils.MakePromRangeAPIQuery(ctx, client, errorRateQuery, startTimeParam, endTimeParam, cfg)
		if err != nil {
			return nil, nil, err
		}
		defer httpResp.Body.Close()

		if httpResp.StatusCode == http.StatusOK {
			data, err := io.ReadAll(httpResp.Body)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to read response body: %w", err)
			}
			details.ErrorRate, err = parsePromTimeSeries(data)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to parse error rate response: %w", err)
			}
		}

		// Calculate Error Percentage over time range as a vector
		errorPercentQuery := fmt.Sprintf(
			"(sum(rate(trace_endpoint_count{service_name='%s', env='%s', span_kind='SPAN_KIND_SERVER', http_status_code=~'4.*|5.*'}[%s])) / sum(rate(trace_endpoint_count{service_name='%s', env='%s', span_kind='SPAN_KIND_SERVER'}[%s])) * 100) default 0",
			serviceName, env, timeRange, serviceName, env, timeRange,
		)
		httpResp, err = utils.MakePromRangeAPIQuery(ctx, client, errorPercentQuery, startTimeParam, endTimeParam, cfg)
		if err != nil {
			return nil, nil, err
		}
		defer httpResp.Body.Close()

		if httpResp.StatusCode == http.StatusOK {
			data, err := io.ReadAll(httpResp.Body)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to read response body: %w", err)
			}
			details.ErrorPercent, err = parsePromTimeSeries(data)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to parse error percent response: %w", err)
			}
		}

		// Get Top 10 Operations by Response Time - keep vector output
		topRTQuery := fmt.Sprintf(
			"topk(10, quantile_over_time(0.95, sum by (span_name, messaging_system, rpc_system, span_kind,net_peer_name,process_runtime_name,db_system)(trace_endpoint_duration{service_name='%s', span_kind!='SPAN_KIND_INTERNAL', env='%s', quantile='p95'}[%s])))",
			serviceName, env, timeRange,
		)
		httpResp, err = utils.MakePromInstantAPIQuery(ctx, client, topRTQuery, endTimeParam, cfg)
		if err != nil {
			return nil, nil, err
		}
		defer httpResp.Body.Close()

		if httpResp.StatusCode == http.StatusOK {
			var topErrResp apiPromInstantResp
			if err := json.NewDecoder(httpResp.Body).Decode(&topErrResp); err == nil {
				details.TopOperations.ByResponseTime = make([]map[string]float64, 0)
				for _, r := range topErrResp {
					// join values of r.Timeseries with a - to create a unique key
					key := fmt.Sprintf("%s-%s-%s-%s-%s-%s-%s",
						r.Metric["span_name"],
						r.Metric["span_kind"],
						r.Metric["net_peer_name"],
						r.Metric["db_system"],
						r.Metric["rpc_system"],
						r.Metric["messaging_system"],
						r.Metric["process_runtime_name"],
					)
					if valStr, ok := r.Value[1].(string); ok {
						val, _ := strconv.ParseFloat(valStr, 64)
						op := make(map[string]float64)
						op[key] = val
						details.TopOperations.ByResponseTime = append(details.TopOperations.ByResponseTime, op)
					}
				}
			}
		}

		// Get Top 10 Operations by Error Rate - keep vector output
		topErrQuery := fmt.Sprintf(
			`sum by (span_name, span_kind, net_peer_name, db_system, rpc_system, messaging_system, process_runtime_name, exception_type)(sum_over_time(trace_client_count{service_name="%s", env='%s', exception_type!=''}[%s])) or
			 sum by (span_name, span_kind, net_peer_name, db_system, rpc_system, messaging_system, process_runtime_name, exception_type)(sum_over_time(trace_endpoint_count{service_name="%s", env='%s', exception_type!=''}[%s])) or
			 sum by (span_name, span_kind, net_peer_name, db_system, rpc_system, messaging_system, process_runtime_name, http_status_code)(sum_over_time(trace_client_count{service_name="%s", env='%s', http_status_code=~"^[45].*"}[%s])) or
			 sum by (span_name, span_kind, net_peer_name, db_system, rpc_system, messaging_system, process_runtime_name, http_status_code)(sum_over_time(trace_endpoint_count{service_name="%s", env='%s', http_status_code=~"^[45].*"}[%s]))`,
			serviceName, env, timeRange, serviceName, env, timeRange, serviceName, env, timeRange, serviceName, env, timeRange,
		)
		httpResp, err = utils.MakePromInstantAPIQuery(ctx, client, topErrQuery, endTimeParam, cfg)
		if err != nil {
			return nil, nil, err
		}
		defer httpResp.Body.Close()

		if httpResp.StatusCode == http.StatusOK {
			var topErrResp apiPromInstantResp
			if err := json.NewDecoder(httpResp.Body).Decode(&topErrResp); err == nil {
				details.TopOperations.ByErrorRate = make([]map[string]int64, 0)
				for _, r := range topErrResp {
					// join values of r.Timeseries with a - to create a unique key
					key := fmt.Sprintf("%s-%s-%s-%s-%s-%s-%s",
						r.Metric["span_name"],
						r.Metric["span_kind"],
						r.Metric["net_peer_name"],
						r.Metric["db_system"],
						r.Metric["rpc_system"],
						r.Metric["messaging_system"],
						r.Metric["process_runtime_name"],
					)
					if valStr, ok := r.Value[1].(string); ok {
						val, _ := strconv.ParseInt(valStr, 10, 64)
						op := make(map[string]int64)
						op[key] = val
						details.TopOperations.ByErrorRate = append(details.TopOperations.ByErrorRate, op)
					}
				}
			}
		}

		// Get Top 10 Errors - keep vector output
		topErrorsQuery := fmt.Sprintf(
			`sum by (exception_type)(sum by (exception_type, span_kind)(sum_over_time(trace_client_count{service_name="%s", env='%s', exception_type!=''}[%s])) or
			 sum by (exception_type, span_kind)(sum_over_time(trace_endpoint_count{service_name="%s", env='%s', exception_type!=''}[%s]))) or
			 sum by (http_status_code)(sum by (http_status_code, span_kind)(sum_over_time(trace_client_count{service_name="%s", env='%s', http_status_code=~"^[45].*"}[%s])) or
			 sum by (http_status_code, span_kind)(sum_over_time(trace_endpoint_count{service_name="%s", env='%s', http_status_code=~"^[45].*"}[%s])))`,
			serviceName, env, timeRange, serviceName, env, timeRange, serviceName, env, timeRange, serviceName, env, timeRange,
		)
		httpResp, err = utils.MakePromInstantAPIQuery(ctx, client, topErrorsQuery, endTimeParam, cfg)
		if err != nil {
			return nil, nil, err
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode == http.StatusOK {
			var topErrResp apiPromInstantResp
			if err := json.NewDecoder(httpResp.Body).Decode(&topErrResp); err == nil {
				details.TopErrors = make([]map[string]int64, 0)
				for _, r := range topErrResp {
					// join values of r.Timeseries with a - to create a unique key
					// extract either exception_type or http_status_code
					var key string
					if exceptionType, ok := r.Metric["exception_type"]; ok && exceptionType != "" {
						key = exceptionType
					} else if httpStatusCode, ok := r.Metric["http_status_code"]; ok && httpStatusCode != "" {
						key = httpStatusCode
					} else {
						continue // skip if neither is present
					}
					if valStr, ok := r.Value[1].(string); ok {
						val, _ := strconv.ParseInt(valStr, 10, 64)
						op := make(map[string]int64)
						op[key] = val
						details.TopErrors = append(details.TopErrors, op)
					}
				}
			}
		}

		resultJSON, err := json.Marshal(details)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to marshal response: %w", err)
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: string(resultJSON),
				},
			},
		}, nil, nil
	}
}

const GetServiceOperationsSummaryDescription = `
	Get a summary of operations inside a service over a given time range.
	Returns a list of operations with their details.
	These include operations like HTTP endpoints, database queries, messaging producer and http client calls.
	Includes service name, environment, throughput, error rate, and response time for each operation.
	All values are p95 quantiles over the time range.
	Response times are in milliseconds. Throughput and error rates are in requests per minute (rpm).
	Each operation includes:
		- operation name
		- service name
		- environment
		- throughput in requests per minute (rpm)
		- error rate in requests per minute (rpm)
		- response time in milliseconds (p95, p90, p50 quantiles and avg)
		- error percentage
	Database operations contain additional fields:
		- db_system: Database system (e.g., mysql, postgres, etc.)
		- net_peer_name: Database host or connection string
	Messaging operations contain additional fields:
		- messaging_system: Messaging system (e.g., kafka, rabbitmq, etc.)
		- net_peer_name: Messaging host or connection string
	HTTP client operations contain additional fields:
		- http_method: HTTP method (e.g., GET, POST, etc.)
		- net_peer_name: HTTP host or connection string
	
	Parameters:
	- start_time: (Required) Start time of the time range in ISO format.
	- end_time: (Required) End time of the time range in ISO format.
	- env: (Required) Environment to filter by. Use "get_service_environments" tool to get available environments.
	- service_name: (Required) Service name to filter by. Defaults to all services.
`

func NewServiceOperationsSummaryHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, ServiceOperationsSummaryArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args ServiceOperationsSummaryArgs) (*mcp.CallToolResult, any, error) {
		// Parse time parameters
		var (
			startTimeParam, endTimeParam int64
		)

		// Handle end_time
		if args.EndTimeISO != "" {
			t, err := time.Parse(time.RFC3339, args.EndTimeISO)
			if err != nil {
				return nil, nil, fmt.Errorf("invalid end_time_iso format, must be ISO8601: %w", err)
			}
			endTimeParam = t.Unix()
		} else {
			endTimeParam = time.Now().Unix()
		}

		// Handle start_time
		if args.StartTimeISO != "" {
			t, err := time.Parse(time.RFC3339, args.StartTimeISO)
			if err != nil {
				return nil, nil, fmt.Errorf("invalid start_time_iso format, must be ISO8601: %w", err)
			}
			startTimeParam = t.Unix()
		} else {
			startTimeParam = endTimeParam - 3600 // default to last hour
		}

		env := args.Env
		if env == "" {
			env = ".*" // default environment
		}
		serviceName := args.ServiceName
		if serviceName == "" {
			return nil, nil, fmt.Errorf("service_name is required")
		}
		timeRange := fmt.Sprintf("%dm", int((endTimeParam-startTimeParam)/60))
		// Prepare the Prometheus query for throughput of endpoint operations
		throughputQuery := fmt.Sprintf(
			"sum by (span_name, span_kind)(sum_over_time(trace_endpoint_count{service_name='%s', span_kind='SPAN_KIND_SERVER', env=~'%s'}[%s])) / %d",
			serviceName, env, timeRange, int((endTimeParam-startTimeParam)/60),
		)
		// Prepare instant query request to Prometheus
		httpResp, err := utils.MakePromInstantAPIQuery(ctx, client, throughputQuery, endTimeParam, cfg)
		if err != nil {
			return nil, nil, err
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode != http.StatusOK {
			return nil, nil, fmt.Errorf("failed to get service operations summary: %s", httpResp.Status)
		}
		var promResp apiPromInstantResp
		if err := json.NewDecoder(httpResp.Body).Decode(&promResp); err != nil {
			return nil, nil, fmt.Errorf("failed to decode Prometheus response: %w", err)
		}
		// Prepare the Prometheus query for response times of endpoint operations
		respTimeQuery := fmt.Sprintf(
			"quantile_over_time(0.95, sum by (quantile, span_name, span_kind) (trace_endpoint_duration{service_name='%s', span_kind='SPAN_KIND_SERVER', env='%s'}[%s]))",
			serviceName, env, timeRange,
		)
		// Prepare request to Prometheus (or your metrics backend)
		httpResp, err = utils.MakePromInstantAPIQuery(ctx, client, respTimeQuery, endTimeParam, cfg)
		if err != nil {
			return nil, nil, err
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode != http.StatusOK {
			return nil, nil, fmt.Errorf("failed to get service operations summary: %s", httpResp.Status)
		}
		var respTimeRaw apiPromInstantResp
		if err := json.NewDecoder(httpResp.Body).Decode(&respTimeRaw); err != nil {
			return nil, nil, fmt.Errorf("failed to decode Prometheus response: %w", err)
		}
		// Prepare the Prometheus query for error rate of endpoint operations
		errorRateQuery := fmt.Sprintf(
			"100 * (sum by (span_name, span_kind) (sum_over_time(trace_endpoint_count{service_name='%s', span_kind='SPAN_KIND_SERVER', env=~'%s', http_status_code=~'4.*|5.*'}[%s])) / %d) / (sum by (span_name, span_kind) (sum_over_time(trace_endpoint_count{service_name='%s', span_kind='SPAN_KIND_SERVER', env=~'%s'}[%s])) / %d)",
			serviceName, env, timeRange, int((endTimeParam-startTimeParam)/60),
			serviceName, env, timeRange, int((endTimeParam-startTimeParam)/60),
		)
		// Prepare request to Prometheus (or your metrics backend)
		httpResp, err = utils.MakePromInstantAPIQuery(ctx, client, errorRateQuery, endTimeParam, cfg)
		if err != nil {
			return nil, nil, err
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode != http.StatusOK {
			return nil, nil, fmt.Errorf("failed to get service operations summary: %s", httpResp.Status)
		}
		var errorRateRaw apiPromInstantResp
		if err := json.NewDecoder(httpResp.Body).Decode(&errorRateRaw); err != nil {
			return nil, nil, fmt.Errorf("failed to decode Prometheus response: %w", err)
		}
		// Prepare the Prometheus query for throughput of database operations
		dbThroughputQuery := fmt.Sprintf(
			"sum by (span_name, db_system, net_peer_name, rpc_system, span_kind)(sum_over_time(trace_client_count{service_name='%s', span_kind='SPAN_KIND_CLIENT', db_system!='', env=~'%s'}[%s])) / %d",
			serviceName, env, timeRange, int((endTimeParam-startTimeParam)/60),
		)
		// Prepare request to Prometheus (or your metrics backend)
		httpResp, err = utils.MakePromInstantAPIQuery(ctx, client, dbThroughputQuery, endTimeParam, cfg)
		if err != nil {
			return nil, nil, err
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode != http.StatusOK {
			return nil, nil, fmt.Errorf("failed to get service operations summary: %s", httpResp.Status)
		}
		var dbThroughputRaw apiPromInstantResp
		if err := json.NewDecoder(httpResp.Body).Decode(&dbThroughputRaw); err != nil {
			return nil, nil, fmt.Errorf("failed to decode Prometheus response: %w", err)
		}
		// Prepare the Prometheus query for response times of database operations
		dbRespTimeQuery := fmt.Sprintf(
			"quantile_over_time(0.95, sum by (quantile, span_name, db_system, net_peer_name, rpc_system, span_kind) (trace_client_duration{service_name='%s', span_kind='SPAN_KIND_CLIENT', db_system!='', env='%s'}[%s]))",
			serviceName, env, timeRange,
		)
		// Prepare request to Prometheus (or your metrics backend)
		httpResp, err = utils.MakePromInstantAPIQuery(ctx, client, dbRespTimeQuery, endTimeParam, cfg)
		if err != nil {
			return nil, nil, err
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode != http.StatusOK {
			return nil, nil, fmt.Errorf("failed to get service operations summary: %s", httpResp.Status)
		}
		var dbRespTimeRaw apiPromInstantResp
		if err := json.NewDecoder(httpResp.Body).Decode(&dbRespTimeRaw); err != nil {
			return nil, nil, fmt.Errorf("failed to decode Prometheus response: %w", err)
		}
		// Prepare the Prometheus query for error rate of database operations
		dbErrorRateQuery := fmt.Sprintf(
			`
			    100 * 
    			(
					sum by(span_name, db_system, messaging_system, net_peer_name, rpc_system, span_kind)
						(sum_over_time(trace_client_count{service_name="%s", db_system!="",env="%s", status_code=~"STATUS_CODE_ERROR"} [%s]) / %d)
					or
					sum by(span_name, db_system, messaging_system, net_peer_name, rpc_system, span_kind)
						(sum_over_time(trace_client_count{service_name="%s", db_system!="",env="%s", http_status_code=~"4.*|5.*"} [%s]) / %d)
				)  
				/ 
				(
					sum by(span_name, db_system, messaging_system, net_peer_name, rpc_system, span_kind)
						(sum_over_time(trace_client_count{service_name="%s", db_system!="",env="%s"} [%s]) / %d)
				)
			`,
			serviceName, env, timeRange, int((endTimeParam-startTimeParam)/60),
			serviceName, env, timeRange, int((endTimeParam-startTimeParam)/60),
			serviceName, env, timeRange, int((endTimeParam-startTimeParam)/60),
		)
		// Prepare request to Prometheus (or your metrics backend)
		httpResp, err = utils.MakePromInstantAPIQuery(ctx, client, dbErrorRateQuery, endTimeParam, cfg)
		if err != nil {
			return nil, nil, err
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode != http.StatusOK {
			return nil, nil, fmt.Errorf("failed to get service operations summary: %s", httpResp.Status)
		}
		var dbErrorRateRaw apiPromInstantResp
		if err := json.NewDecoder(httpResp.Body).Decode(&dbErrorRateRaw); err != nil {
			return nil, nil, fmt.Errorf("failed to decode Prometheus response: %w", err)
		}
		// Prepare query for http operations
		httpThroughputQuery := fmt.Sprintf(
			"sum by(span_name, db_system, net_peer_name, rpc_system, span_kind)(sum_over_time(trace_client_count{service_name='%s', span_kind='SPAN_KIND_CLIENT', env=~'%s'}[%s])) / %d",
			serviceName, env, timeRange, int((endTimeParam-startTimeParam)/60),
		)
		// Prepare request to Prometheus (or your metrics backend)
		httpResp, err = utils.MakePromInstantAPIQuery(ctx, client, httpThroughputQuery, endTimeParam, cfg)
		if err != nil {
			return nil, nil, err
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode != http.StatusOK {
			return nil, nil, fmt.Errorf("failed to get service operations summary: %s", httpResp.Status)
		}
		var httpThroughputRaw apiPromInstantResp
		if err := json.NewDecoder(httpResp.Body).Decode(&httpThroughputRaw); err != nil {
			return nil, nil, fmt.Errorf("failed to decode Prometheus response: %w", err)
		}
		// Prepare the Prometheus query for response times of http operations
		httpRespTimeQuery := fmt.Sprintf(
			"quantile_over_time(0.95, sum by (quantile, span_name, net_peer_name, rpc_system, span_kind) (trace_client_duration{service_name='%s', span_kind='SPAN_KIND_CLIENT', env='%s'}[%s]))",
			serviceName, env, timeRange,
		)
		// Prepare request to Prometheus (or your metrics backend)
		httpResp, err = utils.MakePromInstantAPIQuery(ctx, client, httpRespTimeQuery, endTimeParam, cfg)
		if err != nil {
			return nil, nil, err
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode != http.StatusOK {
			return nil, nil, fmt.Errorf("failed to get service operations summary: %s", httpResp.Status)
		}
		var httpRespTimeRaw apiPromInstantResp
		if err := json.NewDecoder(httpResp.Body).Decode(&httpRespTimeRaw); err != nil {
			return nil, nil, fmt.Errorf("failed to decode Prometheus response: %w", err)
		}
		// Prepare the Prometheus query for error rate of http operations
		httpErrorRateQuery := fmt.Sprintf(
			`			100 * 
			(
				sum by(span_name, db_system, messaging_system, net_peer_name, rpc_system, span_kind)
					(sum_over_time(trace_client_count{service_name="%s", env="%s", status_code=~"STATUS_CODE_ERROR"} [%s]) / %d)
				or
				sum by(span_name, db_system, messaging_system, net_peer_name, rpc_system, span_kind)
					(sum_over_time(trace_client_count{service_name="%s", env="%s", http_status_code=~"4.*|5.*"} [%s]) / %d)
			)
			/
			(
				sum by(span_name, db_system, messaging_system, net_peer_name, rpc_system, span_kind)
					(sum_over_time(trace_client_count{service_name="%s", env="%s"} [%s]) / %d)
			)`,
			serviceName, env, timeRange, int((endTimeParam-startTimeParam)/60),
			serviceName, env, timeRange, int((endTimeParam-startTimeParam)/60),
			serviceName, env, timeRange, int((endTimeParam-startTimeParam)/60),
		)
		// Prepare request to Prometheus (or your metrics backend)
		httpResp, err = utils.MakePromInstantAPIQuery(ctx, client, httpErrorRateQuery, endTimeParam, cfg)
		if err != nil {
			return nil, nil, err
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode != http.StatusOK {
			return nil, nil, fmt.Errorf("failed to get service operations summary: %s", httpResp.Status)
		}
		var httpErrorRateRaw apiPromInstantResp
		if err := json.NewDecoder(httpResp.Body).Decode(&httpErrorRateRaw); err != nil {
			return nil, nil, fmt.Errorf("failed to decode Prometheus response: %w", err)
		}
		// Prepare query for messaging operations
		messagingThroughputQuery := fmt.Sprintf(
			"sum by(span_name, messaging_system, net_peer_name, rpc_system, span_kind)(sum_over_time(trace_client_count{service_name='%s', messaging_system!='', span_kind='SPAN_KIND_PRODUCER', env=~'%s'}[%s])) / %d",
			serviceName, env, timeRange, int((endTimeParam-startTimeParam)/60),
		)
		// Prepare request to Prometheus (or your metrics backend)
		httpResp, err = utils.MakePromInstantAPIQuery(ctx, client, messagingThroughputQuery, endTimeParam, cfg)
		if err != nil {
			return nil, nil, err
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode != http.StatusOK {
			return nil, nil, fmt.Errorf("failed to get service operations summary: %s", httpResp.Status)
		}
		var messagingThroughputRaw apiPromInstantResp
		if err := json.NewDecoder(httpResp.Body).Decode(&messagingThroughputRaw); err != nil {
			return nil, nil, fmt.Errorf("failed to decode Prometheus response: %w", err)
		}
		// Prepare the Prometheus query for response times of messaging operations
		messagingRespTimeQuery := fmt.Sprintf(
			"quantile_over_time(0.95, sum by (quantile, span_name, messaging_system, net_peer_name, rpc_system, span_kind) (trace_client_duration{service_name='%s', messaging_system!='', span_kind='SPAN_KIND_PRODUCER', env='%s'}[%s]))",
			serviceName, env, timeRange,
		)
		// Prepare request to Prometheus (or your metrics backend)
		httpResp, err = utils.MakePromInstantAPIQuery(ctx, client, messagingRespTimeQuery, endTimeParam, cfg)
		if err != nil {
			return nil, nil, err
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode != http.StatusOK {
			return nil, nil, fmt.Errorf("failed to get service operations summary: %s", httpResp.Status)
		}
		var messagingRespTimeRaw apiPromInstantResp
		if err := json.NewDecoder(httpResp.Body).Decode(&messagingRespTimeRaw); err != nil {
			return nil, nil, fmt.Errorf("failed to decode Prometheus response: %w", err)
		}
		// Prepare the Prometheus query for error rate of messaging operations
		messagingErrorRateQuery := fmt.Sprintf(
			`			100 * 
			(
				sum by(span_name, messaging_system, net_peer_name, rpc_system, span_kind)
					(sum_over_time(trace_client_count{service_name="%s", messaging_system!="", env="%s", status_code=~"STATUS_CODE_ERROR", span_kind='SPAN_KIND_PRODUCER'} [%s]) / %d)
				or
				sum by(span_name, messaging_system, net_peer_name, rpc_system, span_kind)
					(sum_over_time(trace_client_count{service_name="%s", messaging_system!="", env="%s", http_status_code=~"4.*|5.*", span_kind='SPAN_KIND_PRODUCER'} [%s]) / %d)
			)
			/
			(
				sum by(span_name, messaging_system, net_peer_name, rpc_system, span_kind)
					(sum_over_time(trace_client_count{service_name="%s", messaging_system!="", env="%s", span_kind='SPAN_KIND_PRODUCER'} [%s]) / %d)
			)`,
			serviceName, env, timeRange, int((endTimeParam-startTimeParam)/60),
			serviceName, env, timeRange, int((endTimeParam-startTimeParam)/60),
			serviceName, env, timeRange, int((endTimeParam-startTimeParam)/60),
		)
		// Prepare request to Prometheus (or your metrics backend)
		httpResp, err = utils.MakePromInstantAPIQuery(ctx, client, messagingErrorRateQuery, endTimeParam, cfg)
		if err != nil {
			return nil, nil, err
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode != http.StatusOK {
			return nil, nil, fmt.Errorf("failed to get service operations summary: %s", httpResp.Status)
		}
		var messagingErrorRateRaw apiPromInstantResp
		if err := json.NewDecoder(httpResp.Body).Decode(&messagingErrorRateRaw); err != nil {
			return nil, nil, fmt.Errorf("failed to decode Prometheus response: %w", err)
		}
		// Prepare the response structure
		operationsSummary := make([]ServiceOperationSummary, 0)
		for _, r := range promResp {
			// Extract operation details
			operation := ServiceOperationSummary{
				Name:        r.Metric["span_name"],
				ServiceName: serviceName,
				Env:         env,
				Throughput:  0, // default to 0, will be updated later
				ErrorRate:   0, // default to 0, will be updated later
				ResponseTime: map[string]float64{
					"p95": 0, // default to 0, will be updated later
					"p90": 0,
					"p50": 0,
					"avg": 0,
				},
				ErrorPercent: 0, // default to 0, will be updated later
			}
			if valStr, ok := r.Value[1].(string); ok {
				if throughputVal, err := strconv.ParseFloat(valStr, 64); err == nil {
					operation.Throughput = throughputVal
				}
			}
			// Find matching response time data
			for _, rt := range respTimeRaw {
				if rt.Metric["span_name"] == operation.Name {
					quantile, ok := rt.Metric["quantile"]
					if !ok {
						continue // skip if quantile is not present
					}
					if valStr, ok := rt.Value[1].(string); ok {
						if val, err := strconv.ParseFloat(valStr, 64); err == nil {
							// Update the response time for the corresponding quantile
							operation.ResponseTime[quantile] = val
						}
					}
				}
			}

			// Find matching error rate data
			for _, er := range errorRateRaw {
				if er.Metric["span_name"] == operation.Name {
					if valStr, ok := er.Value[1].(string); ok {
						if errorRateVal, err := strconv.ParseFloat(valStr, 64); err == nil {
							operation.ErrorRate = errorRateVal
						}
					}
				}
			}
			// Calculate error percentage
			if operation.Throughput > 0 {
				operation.ErrorPercent = (operation.ErrorRate / operation.Throughput) * 100
			}
		}
		// Add database operations
		for _, r := range dbThroughputRaw {
			// Extract operation details
			operation := ServiceOperationSummary{
				Name:        r.Metric["span_name"],
				ServiceName: serviceName,
				Env:         env,
				DBSystem:    r.Metric["db_system"],
				NetPeerName: r.Metric["net_peer_name"],
				Throughput:  0, // default to 0, will be updated later
				ErrorRate:   0, // default to 0, will be updated later
				ResponseTime: map[string]float64{
					"p95": 0, // default to 0, will be updated later
					"p90": 0,
					"p50": 0,
					"avg": 0,
				},
				ErrorPercent: 0, // default to 0, will be updated later
			}
			if valStr, ok := r.Value[1].(string); ok {
				if throughputVal, err := strconv.ParseFloat(valStr, 64); err == nil {
					operation.Throughput = throughputVal
				}
			}
			// Find matching response time data
			for _, rt := range dbRespTimeRaw {
				if rt.Metric["span_name"] == operation.Name &&
					rt.Metric["db_system"] == operation.DBSystem &&
					rt.Metric["net_peer_name"] == operation.NetPeerName {
					quantile, ok := rt.Metric["quantile"]
					if !ok {
						continue // skip if quantile is not present
					}
					if valStr, ok := rt.Value[1].(string); ok {
						if val, err := strconv.ParseFloat(valStr, 64); err == nil {
							// Update the response time for the corresponding quantile
							operation.ResponseTime[quantile] = val
						}
					}
				}
			}
			// Find matching error rate data
			for _, er := range dbErrorRateRaw {
				if er.Metric["span_name"] == operation.Name &&
					er.Metric["db_system"] == operation.DBSystem &&
					er.Metric["net_peer_name"] == operation.NetPeerName {
					if valStr, ok := er.Value[1].(string); ok {
						if errorRateVal, err := strconv.ParseFloat(valStr, 64); err == nil {
							operation.ErrorRate = errorRateVal
						}
					}
				}
			}
			// Calculate error percentage
			if operation.Throughput > 0 {
				operation.ErrorPercent = (operation.ErrorRate / operation.Throughput) * 100
			}
			operationsSummary = append(operationsSummary, operation)
		}
		// add http operations
		for _, r := range httpThroughputRaw {
			// Extract operation details
			operation := ServiceOperationSummary{
				Name:        r.Metric["span_name"],
				ServiceName: serviceName,
				Env:         env,
				NetPeerName: r.Metric["net_peer_name"],
				RPCSystem:   r.Metric["rpc_system"],
				Throughput:  0, // default to 0, will be updated later
				ErrorRate:   0, // default to 0, will be updated later
				ResponseTime: map[string]float64{
					"p95": 0, // default to 0, will be updated later
					"p90": 0,
					"p50": 0,
					"avg": 0,
				},
				ErrorPercent: 0, // default to 0, will be updated later
			}
			if valStr, ok := r.Value[1].(string); ok {
				if throughputVal, err := strconv.ParseFloat(valStr, 64); err == nil {
					operation.Throughput = throughputVal
				}
			}
			// Find matching response time data
			for _, rt := range httpRespTimeRaw {
				if rt.Metric["span_name"] == operation.Name &&
					rt.Metric["net_peer_name"] == operation.NetPeerName &&
					rt.Metric["rpc_system"] == operation.RPCSystem {
					quantile, ok := rt.Metric["quantile"]
					if !ok {
						continue // skip if quantile is not present
					}
					if valStr, ok := rt.Value[1].(string); ok {
						if val, err := strconv.ParseFloat(valStr, 64); err == nil {
							// Update the response time for the corresponding quantile
							operation.ResponseTime[quantile] = val
						}
					}
				}
			}
			// Find matching error rate data
			for _, er := range httpErrorRateRaw {
				if er.Metric["span_name"] == operation.Name &&
					er.Metric["net_peer_name"] == operation.NetPeerName &&
					er.Metric["rpc_system"] == operation.RPCSystem {
					if valStr, ok := er.Value[1].(string); ok {
						if errorRateVal, err := strconv.ParseFloat(valStr, 64); err == nil {
							operation.ErrorRate = errorRateVal
						}
					}
				}
			}
			// Calculate error percentage
			if operation.Throughput > 0 {
				operation.ErrorPercent = (operation.ErrorRate / operation.Throughput) * 100
			}
			operationsSummary = append(operationsSummary, operation)
		}
		// add messaging operations
		for _, r := range messagingThroughputRaw {
			// Extract operation details
			operation := ServiceOperationSummary{
				Name:            r.Metric["span_name"],
				ServiceName:     serviceName,
				Env:             env,
				MessagingSystem: r.Metric["messaging_system"],
				NetPeerName:     r.Metric["net_peer_name"],
				RPCSystem:       r.Metric["rpc_system"],
				Throughput:      0, // default to 0, will be updated later
				ErrorRate:       0, // default to 0, will be updated later
				ResponseTime: map[string]float64{
					"p95": 0, // default to 0, will be updated later
					"p90": 0,
					"p50": 0,
					"avg": 0,
				},
				ErrorPercent: 0, // default to 0, will be updated later
			}
			if valStr, ok := r.Value[1].(string); ok {
				if throughputVal, err := strconv.ParseFloat(valStr, 64); err == nil {
					operation.Throughput = throughputVal
				}
			}
			// Find matching response time data
			for _, rt := range messagingRespTimeRaw {
				if rt.Metric["span_name"] == operation.Name &&
					rt.Metric["messaging_system"] == operation.MessagingSystem &&
					rt.Metric["net_peer_name"] == operation.NetPeerName &&
					rt.Metric["rpc_system"] == operation.RPCSystem {
					quantile, ok := rt.Metric["quantile"]
					if !ok {
						continue // skip if quantile is not present
					}
					if valStr, ok := rt.Value[1].(string); ok {
						if val, err := strconv.ParseFloat(valStr, 64); err == nil {
							// Update the response time for the corresponding quantile
							operation.ResponseTime[quantile] = val
						}
					}
				}
			}
			// Find matching error rate data
			for _, er := range messagingErrorRateRaw {
				if er.Metric["span_name"] == operation.Name &&
					er.Metric["messaging_system"] == operation.MessagingSystem &&
					er.Metric["net_peer_name"] == operation.NetPeerName &&
					er.Metric["rpc_system"] == operation.RPCSystem {
					if valStr, ok := er.Value[1].(string); ok {
						if errorRateVal, err := strconv.ParseFloat(valStr, 64); err == nil {
							operation.ErrorRate = errorRateVal
						}
					}
				}
			}
			// Calculate error percentage
			if operation.Throughput > 0 {
				operation.ErrorPercent = (operation.ErrorRate / operation.Throughput) * 100
			}
			operationsSummary = append(operationsSummary, operation)
		}
		// Prepare the final response structure
		details := ServiceOperationsSummaryResponse{
			ServiceName: serviceName,
			Env:         env,
			Operations:  operationsSummary,
		}
		// Return the response
		resultJSON, err := json.Marshal(details)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to marshal response: %w", err)
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: string(resultJSON),
				},
			},
		}, nil, nil
	}
}

type RedMetrics struct {
	Throughput, ResponseTimeP95, ErrorRate, ErrorPercent float64
	ResponseTimeP50, ResponseTimeP90, ResponseTimeAvg    float64
}

type ServiceDependencyGraphDetails struct {
	ServiceName      string                `json:"service_name"`
	Env              string                `json:"env"`
	Incoming         map[string]RedMetrics `json:"incoming"`
	Outgoing         map[string]RedMetrics `json:"outgoing"`
	MessagingSystems map[string]RedMetrics `json:"messaging_systems"`
	Databases        map[string]RedMetrics `json:"databases"`
}

const GetServiceDependencyGraphDetails = `
	Get details of the throughput, response times and error rates of
	incoming, outgoing and infrastructure components like messaging and databases
	of a service.
	This tool can be used to get a detailed dependency graph of a service and help
	in analysis of cascading effect of errors and performance issues.
	It returns a structured response with the following fields:
	- service name
	- environment
	- throughput in requests per minute (rpm)
	- error rate in requests per minute (rpm)
	- p95 response time in milliseconds
	- p90 response time in milliseconds
	- p50 response time in milliseconds
	- avg response time in milliseconds
	- error percentage
	The detailed metrics, error rates and operation details of incoming and outgoing dependencies
	can be obtained by using the get_service_details tool.
	In the parameters, it is recommended to use the ISO8601 format for start_time and end_time,
	with a time window of 1 hour.
	Parameters:
	- start_time: (Required) Start time of the time range in ISO format.
	- end_time: (Required) End time of the time range in ISO format.
	- env: (Required) Environment to filter by. Use "get_service_environments" tool to get available environments.
	- service_name: (Required) Name of the service to get the dependency graph for.
	`

func NewServiceDependencyGraphHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, ServiceDependencyGraphArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args ServiceDependencyGraphArgs) (*mcp.CallToolResult, any, error) {
		// Parse time parameters
		var (
			startTimeParam, endTimeParam int64
		)

		// Handle end_time
		if args.EndTimeISO != "" {
			t, err := time.Parse(time.RFC3339, args.EndTimeISO)
			if err != nil {
				return nil, nil, fmt.Errorf("invalid end_time_iso format, must be ISO8601: %w", err)
			}
			endTimeParam = t.Unix()
		} else {
			endTimeParam = time.Now().Unix()
		}

		// Handle start_time
		if args.StartTimeISO != "" {
			t, err := time.Parse(time.RFC3339, args.StartTimeISO)
			if err != nil {
				return nil, nil, fmt.Errorf("invalid start_time_iso format, must be ISO8601: %w", err)
			}
			startTimeParam = t.Unix()
		} else {
			startTimeParam = endTimeParam - 3600 // default to last hour
		}

		env := args.Env
		if env == "" {
			env = ".*" // default environment
		}
		serviceName := args.ServiceName
		if serviceName == "" {
			return nil, nil, fmt.Errorf("service_name is required")
		}
		timeRange := fmt.Sprintf("%dm", int((endTimeParam-startTimeParam)/60))

		incoming := make(map[string]RedMetrics)
		outgoing := make(map[string]RedMetrics)
		databases := make(map[string]RedMetrics)
		messagingSystems := make(map[string]RedMetrics)

		// Incoming requests (HTTP server operations):
		// throughput
		incomingThroughputQuery := fmt.Sprintf(
			"sum by (client)(sum_over_time(trace_call_graph_count{server='%s', env=~'%s'}[%s])) / %d",
			serviceName, env, timeRange, int((endTimeParam-startTimeParam)/60),
		)
		httpResp, err := utils.MakePromInstantAPIQuery(ctx, client, incomingThroughputQuery, endTimeParam, cfg)
		if err != nil {
			return nil, nil, err
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode != http.StatusOK {
			return nil, nil, fmt.Errorf("failed to get service dependency graph details: %s", httpResp.Status)
		}
		var incomingThroughputRaw apiPromInstantResp
		if err := json.NewDecoder(httpResp.Body).Decode(&incomingThroughputRaw); err != nil {
			return nil, nil, fmt.Errorf("failed to decode Prometheus response: %w", err)
		}
		// response times
		incomingRespTimeQuery := fmt.Sprintf(
			"quantile_over_time(0.95 ,sum by (client, quantile) (trace_call_graph_duration{server='%s', env=~'%s'}[%s]))",
			serviceName, env, timeRange,
		)
		httpResp, err = utils.MakePromInstantAPIQuery(ctx, client, incomingRespTimeQuery, endTimeParam, cfg)
		if err != nil {
			return nil, nil, err
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode != http.StatusOK {
			return nil, nil, fmt.Errorf("failed to get service dependency graph details: %s", httpResp.Status)
		}
		var incomingRespTimeRaw apiPromInstantResp
		if err := json.NewDecoder(httpResp.Body).Decode(&incomingRespTimeRaw); err != nil {
			return nil, nil, fmt.Errorf("failed to decode Prometheus response: %w", err)
		}
		// error rate
		incomingErrorRateQuery := fmt.Sprintf(
			"sum by (client)(sum_over_time(trace_call_graph_count{server='%s', env=~'%s', client_status=~'4.*|5.*'}[%s])) / %d",
			serviceName, env, timeRange, int((endTimeParam-startTimeParam)/60),
		)
		httpResp, err = utils.MakePromInstantAPIQuery(ctx, client, incomingErrorRateQuery, endTimeParam, cfg)
		if err != nil {
			return nil, nil, err
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode != http.StatusOK {
			return nil, nil, fmt.Errorf("failed to get service dependency graph details: %s", httpResp.Status)
		}
		var incomingErrorRateRaw apiPromInstantResp
		if err := json.NewDecoder(httpResp.Body).Decode(&incomingErrorRateRaw); err != nil {
			return nil, nil, fmt.Errorf("failed to decode Prometheus response: %w", err)
		}
		// Process incoming data
		for _, r := range incomingThroughputRaw {
			client := r.Metric["client"]
			if client == "" {
				client = "unknown"
			}
			metrics := RedMetrics{}
			if valStr, ok := r.Value[1].(string); ok {
				if throughputVal, err := strconv.ParseFloat(valStr, 64); err == nil {
					metrics.Throughput = throughputVal
				}
			}
			incoming[client] = metrics
		}
		for _, r := range incomingRespTimeRaw {
			client := r.Metric["client"]
			if client == "" {
				client = "unknown"
			}
			quantile := r.Metric["quantile"]
			metrics := incoming[client]
			if valStr, ok := r.Value[1].(string); ok {
				if val, err := strconv.ParseFloat(valStr, 64); err == nil {
					switch quantile {
					case "p95":
						metrics.ResponseTimeP95 = val
					case "p90":
						metrics.ResponseTimeP90 = val
					case "p50":
						metrics.ResponseTimeP50 = val
					case "avg":
						metrics.ResponseTimeAvg = val
					}
				}
			}
			incoming[client] = metrics
		}
		for _, r := range incomingErrorRateRaw {
			client := r.Metric["client"]
			if client == "" {
				client = "unknown"
			}
			metrics := incoming[client]
			if valStr, ok := r.Value[1].(string); ok {
				if errorRateVal, err := strconv.ParseFloat(valStr, 64); err == nil {
					metrics.ErrorRate = errorRateVal
				}
			}
			incoming[client] = metrics
		}
		for client, metrics := range incoming {
			if metrics.Throughput > 0 {
				metrics.ErrorPercent = (metrics.ErrorRate / metrics.Throughput) * 100
			}
			incoming[client] = metrics
		}
		// Outgoing requests (HTTP client operations):
		// throughput
		outgoingThroughputQuery := fmt.Sprintf(
			"sum by (server)(sum_over_time(trace_call_graph_count{client='%s', env=~'%s'}[%s])) / %d",
			serviceName, env, timeRange, int((endTimeParam-startTimeParam)/60),
		)
		httpResp, err = utils.MakePromInstantAPIQuery(ctx, client, outgoingThroughputQuery, endTimeParam, cfg)
		if err != nil {
			return nil, nil, err
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode != http.StatusOK {
			return nil, nil, fmt.Errorf("failed to get service dependency graph details: %s", httpResp.Status)
		}
		var outgoingThroughputRaw apiPromInstantResp
		if err := json.NewDecoder(httpResp.Body).Decode(&outgoingThroughputRaw); err != nil {
			return nil, nil, fmt.Errorf("failed to decode Prometheus response: %w", err)
		}
		// response times
		outgoingRespTimeQuery := fmt.Sprintf(
			"quantile_over_time(0.95 ,sum by (server, quantile) (trace_call_graph_duration{client='%s', env=~'%s'}[%s]))",
			serviceName, env, timeRange,
		)
		httpResp, err = utils.MakePromInstantAPIQuery(ctx, client, outgoingRespTimeQuery, endTimeParam, cfg)
		if err != nil {
			return nil, nil, err
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode != http.StatusOK {
			return nil, nil, fmt.Errorf("failed to get service dependency graph details: %s", httpResp.Status)
		}
		var outgoingRespTimeRaw apiPromInstantResp
		if err := json.NewDecoder(httpResp.Body).Decode(&outgoingRespTimeRaw); err != nil {
			return nil, nil, fmt.Errorf("failed to decode Prometheus response: %w", err)
		}
		// error rate
		outgoingErrorRateQuery := fmt.Sprintf(
			"sum by (server)(sum_over_time(trace_call_graph_count{client='%s', env=~'%s', client_status=~'4.*|5.*'}[%s])) / %d",
			serviceName, env, timeRange, int((endTimeParam-startTimeParam)/60),
		)
		httpResp, err = utils.MakePromInstantAPIQuery(ctx, client, outgoingErrorRateQuery, endTimeParam, cfg)
		if err != nil {
			return nil, nil, err
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode != http.StatusOK {
			return nil, nil, fmt.Errorf("failed to get service dependency graph details: %s", httpResp.Status)
		}
		var outgoingErrorRateRaw apiPromInstantResp
		if err := json.NewDecoder(httpResp.Body).Decode(&outgoingErrorRateRaw); err != nil {
			return nil, nil, fmt.Errorf("failed to decode Prometheus response: %w", err)
		}
		// Process outgoing data

		for _, r := range outgoingThroughputRaw {
			server := r.Metric["server"]
			if server == "" {
				server = "unknown"
			}
			metrics := RedMetrics{}
			if valStr, ok := r.Value[1].(string); ok {
				if throughputVal, err := strconv.ParseFloat(valStr, 64); err == nil {
					metrics.Throughput = throughputVal
				}
			}
			outgoing[server] = metrics
		}
		for _, r := range outgoingRespTimeRaw {
			server := r.Metric["server"]
			if server == "" {
				server = "unknown"
			}
			quantile := r.Metric["quantile"]
			metrics := outgoing[server]
			if valStr, ok := r.Value[1].(string); ok {
				if val, err := strconv.ParseFloat(valStr, 64); err == nil {
					switch quantile {
					case "p95":
						metrics.ResponseTimeP95 = val
					case "p90":
						metrics.ResponseTimeP90 = val
					case "p50":
						metrics.ResponseTimeP50 = val
					case "avg":
						metrics.ResponseTimeAvg = val
					}
				}
			}
			outgoing[server] = metrics
		}
		for _, r := range outgoingErrorRateRaw {
			server := r.Metric["server"]
			if server == "" {
				server = "unknown"
			}
			metrics := outgoing[server]
			if valStr, ok := r.Value[1].(string); ok {
				if errorRateVal, err := strconv.ParseFloat(valStr, 64); err == nil {
					metrics.ErrorRate = errorRateVal
				}
			}
			outgoing[server] = metrics
		}
		for server, metrics := range outgoing {
			if metrics.Throughput > 0 {
				metrics.ErrorPercent = (metrics.ErrorRate / metrics.Throughput) * 100
			}
			outgoing[server] = metrics
		}
		// Infrastructure services:
		// throughput
		infrastructureThroughputQuery := fmt.Sprintf(
			"sum by (server_host, server_db_system, server_rpc_system, server_messaging_system, server_rpc_service) (sum_over_time(trace_internal_call_graph_count{client='%s', env=~'%s'}[%s])) / %d",
			serviceName, env, timeRange, int((endTimeParam-startTimeParam)/60),
		)
		httpResp, err = utils.MakePromInstantAPIQuery(ctx, client, infrastructureThroughputQuery, endTimeParam, cfg)
		if err != nil {
			return nil, nil, err
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode != http.StatusOK {
			return nil, nil, fmt.Errorf("failed to get service dependency graph details: %s", httpResp.Status)
		}
		var infrastructureThroughputRaw apiPromInstantResp
		if err := json.NewDecoder(httpResp.Body).Decode(&infrastructureThroughputRaw); err != nil {
			return nil, nil, fmt.Errorf("failed to decode Prometheus response: %w", err)
		}
		// response times
		infrastructureRespTimeQuery := fmt.Sprintf(
			"quantile_over_time(0.95 ,sum by (server_host, server_db_system, server_rpc_system, server_messaging_system, server_rpc_service, quantile) (trace_internal_call_graph_duration{client='%s', env=~'%s'}[%s]))",
			serviceName, env, timeRange,
		)
		httpResp, err = utils.MakePromInstantAPIQuery(ctx, client, infrastructureRespTimeQuery, endTimeParam, cfg)
		if err != nil {
			return nil, nil, err
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode != http.StatusOK {
			return nil, nil, fmt.Errorf("failed to get service dependency graph details: %s", httpResp.Status)
		}
		var infrastructureRespTimeRaw apiPromInstantResp
		if err := json.NewDecoder(httpResp.Body).Decode(&infrastructureRespTimeRaw); err != nil {
			return nil, nil, fmt.Errorf("failed to decode Prometheus response: %w", err)
		}
		// error rate
		infrastructureErrorRateQuery := fmt.Sprintf(
			"sum by (server_host, server_db_system, server_rpc_system, server_messaging_system, server_rpc_service) (sum_over_time(trace_internal_call_graph_count{client='%s', env=~'%s', client_status=~'4.*|5.*'}[%s])) / %d",
			serviceName, env, timeRange, int((endTimeParam-startTimeParam)/60),
		)
		httpResp, err = utils.MakePromInstantAPIQuery(ctx, client, infrastructureErrorRateQuery, endTimeParam, cfg)
		if err != nil {
			return nil, nil, err
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode != http.StatusOK {
			return nil, nil, fmt.Errorf("failed to get service dependency graph details: %s", httpResp.Status)
		}
		var infrastructureErrorRateRaw apiPromInstantResp
		if err := json.NewDecoder(httpResp.Body).Decode(&infrastructureErrorRateRaw); err != nil {
			return nil, nil, fmt.Errorf("failed to decode Prometheus response: %w", err)
		}
		// Process infrastructure data
		for _, r := range infrastructureThroughputRaw {
			host := r.Metric["server_host"]
			dbSystem := r.Metric["server_db_system"]
			rpcSystem := r.Metric["server_rpc_system"]
			messagingSystem := r.Metric["server_messaging_system"]
			rpcService := r.Metric["server_rpc_service"]
			key := ""
			metrics := RedMetrics{}
			if dbSystem != "" {
				key = fmt.Sprintf("%s %s", host, dbSystem)
			} else if messagingSystem != "" {
				key = fmt.Sprintf("%s %s %s %s", host, messagingSystem, rpcSystem, rpcService)
			} else {
				continue // skip if neither db_system nor messaging_system is present
			}
			if valStr, ok := r.Value[1].(string); ok {
				if throughputVal, err := strconv.ParseFloat(valStr, 64); err == nil {
					metrics.Throughput = throughputVal
				}
			}
			if dbSystem != "" {
				databases[key] = metrics
			} else if messagingSystem != "" {
				messagingSystems[key] = metrics
			}
		}
		for _, r := range infrastructureRespTimeRaw {
			host := r.Metric["server_host"]
			dbSystem := r.Metric["server_db_system"]
			rpcSystem := r.Metric["server_rpc_system"]
			messagingSystem := r.Metric["server_messaging_system"]
			rpcService := r.Metric["server_rpc_service"]
			quantile := r.Metric["quantile"]
			key := ""
			metrics := RedMetrics{}
			if dbSystem != "" {
				key = fmt.Sprintf("%s %s", host, dbSystem)
				metrics = databases[key]
			} else if messagingSystem != "" {
				key = fmt.Sprintf("%s %s %s %s", host, messagingSystem, rpcSystem, rpcService)
				metrics = messagingSystems[key]
			} else {
				continue // skip if neither db_system nor messaging_system is present
			}
			if valStr, ok := r.Value[1].(string); ok {
				if val, err := strconv.ParseFloat(valStr, 64); err == nil {
					switch quantile {
					case "p95":
						metrics.ResponseTimeP95 = val
					case "p90":
						metrics.ResponseTimeP90 = val
					case "p50":
						metrics.ResponseTimeP50 = val
					case "avg":
						metrics.ResponseTimeAvg = val
					}
				}
			}
			if dbSystem != "" {
				databases[key] = metrics
			} else if messagingSystem != "" {
				messagingSystems[key] = metrics
			}
		}
		for _, r := range infrastructureErrorRateRaw {
			host := r.Metric["server_host"]
			dbSystem := r.Metric["server_db_system"]
			rpcSystem := r.Metric["server_rpc_system"]
			messagingSystem := r.Metric["server_messaging_system"]
			rpcService := r.Metric["server_rpc_service"]
			key := ""
			metrics := RedMetrics{}
			if dbSystem != "" {
				key = fmt.Sprintf("%s %s", host, dbSystem)
				metrics = databases[key]
			} else if messagingSystem != "" {
				key = fmt.Sprintf("%s %s %s %s", host, messagingSystem, rpcSystem, rpcService)
				metrics = messagingSystems[key]
			} else {
				continue // skip if neither db_system nor messaging_system is present
			}
			if valStr, ok := r.Value[1].(string); ok {
				if errorRateVal, err := strconv.ParseFloat(valStr, 64); err == nil {
					metrics.ErrorRate = errorRateVal
				}
			}
			if dbSystem != "" {
				databases[key] = metrics
			} else if messagingSystem != "" {
				messagingSystems[key] = metrics
			}
		}
		for key, metrics := range databases {
			if metrics.Throughput > 0 {
				metrics.ErrorPercent = (metrics.ErrorRate / metrics.Throughput) * 100
			}
			databases[key] = metrics
		}
		for key, metrics := range messagingSystems {
			if metrics.Throughput > 0 {
				metrics.ErrorPercent = (metrics.ErrorRate / metrics.Throughput) * 100
			}
			messagingSystems[key] = metrics
		}
		// Prepare the final response structure
		details := ServiceDependencyGraphDetails{
			ServiceName:      serviceName,
			Env:              env,
			Incoming:         incoming,
			Outgoing:         outgoing,
			Databases:        databases,
			MessagingSystems: messagingSystems,
		}
		// Return the response
		resultJSON, err := json.Marshal(details)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to marshal response: %w", err)
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: string(resultJSON),
				},
			},
		}, nil, nil
	}
}

const PromqlRangeQueryDetails = `
	Perform a Prometheus range query to get metrics data.
	This tool can be used to query Prometheus for metrics data over a specified time range.
	It is recommended to initially check the the available labels on the promql metric using the prometheus_labels tool
	for filtering by a specific environment. Labels like "env", "environment" or "development_environment"
	are common. To get possible values of a label, the prometheus_label_values tool can be used.
	It returns a structured response with the following fields:
	- metric: A map of metric labels and their values.
	- value: A list of lists. Each item in the list has timestamp as the first element
		and the value as the second.
	Example:
	[ {
		"metric": {
			"__name__": "http_request_duration_seconds",
			"method": "GET",
			"status": "200"
		},
		"value": [
			[1700000000, "0.123"],
			[1700000060, "0.456"],
			...
		]
	}]
	The response will contain the metrics data for the specified query.
	Parameters:
	- query: (Required) The Prometheus query to execute.
	- start_time: (Required) Start time of the time range in ISO format.
	- end_time: (Required) End time of the time range in ISO format.
	`

func NewPromqlRangeQueryHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, PromqlRangeQueryArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args PromqlRangeQueryArgs) (*mcp.CallToolResult, any, error) {
		query := args.Query
		if query == "" {
			return nil, nil, fmt.Errorf("query is required")
		}

		var startTimeParam, endTimeParam int64

		// Handle end_time
		if args.EndTimeISO != "" {
			t, err := time.Parse(time.RFC3339, args.EndTimeISO)
			if err != nil {
				return nil, nil, fmt.Errorf("invalid end_time_iso format, must be ISO8601: %w", err)
			}
			endTimeParam = t.Unix()
		} else {
			endTimeParam = time.Now().Unix()
		}

		// Handle start_time
		if args.StartTimeISO != "" {
			t, err := time.Parse(time.RFC3339, args.StartTimeISO)
			if err != nil {
				return nil, nil, fmt.Errorf("invalid start_time_iso format, must be ISO8601: %w", err)
			}
			startTimeParam = t.Unix()
		} else {
			startTimeParam = endTimeParam - 3600 // default to last hour
		}

		httpResp, err := utils.MakePromRangeAPIQuery(ctx, client, query, startTimeParam, endTimeParam, cfg)
		if err != nil {
			return nil, nil, err
		}
		if httpResp == nil {
			return nil, nil, fmt.Errorf("received nil response from Prometheus")
		}
		if httpResp.StatusCode != http.StatusOK {
			return nil, nil, fmt.Errorf("failed to execute Prometheus range query: %s", httpResp.Status)
		}
		defer httpResp.Body.Close()
		// return the response body string as the content without parsing
		responseBodyBytes, err := io.ReadAll(httpResp.Body)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to read response body: %w", err)
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: string(responseBodyBytes),
				},
			},
		}, nil, nil
	}
}

// handler for prometheus instant query
const PromqlInstantQueryDetails = `
	Perform a Prometheus instant query to get metrics data.
	Typically, the query should have rollup functions like sum_over_time, avg_over_time, quantile_over_time, etc
	over a time window. For example: avg_over_time(trace_endpoint_count{env="prod"}[1h])
	This tool can be used to query Prometheus for metrics data at a specific point in time.
	It is recommended to initially check the the available labels on the promql metric using the prometheus_labels tool
	for filtering by a specific environment. Labels like "env", "environment" or "development_environment"
	are common. To get possible values of a label, the prometheus_label_values tool can be used.
	It returns a structured response with the following fields:
	- metric: A map of metric labels and their values.
	- value: A list of lists. Each item in the list has timestamp as the first element
		and the value as the second.
	Response Example:
	[ {
		"metric": {
			"__name__": "http_request_duration_seconds",
			"method": "GET",
			"status": "200"
		},
		"value": [1700000000, "0.123"]
	}]
	The response will contain the metrics data for the specified query.
	Parameters:
	- query: (Required) The Prometheus query to execute.
	- time: (Required) The point in time to query in ISO format.
`

func NewPromqlInstantQueryHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, PromqlInstantQueryArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args PromqlInstantQueryArgs) (*mcp.CallToolResult, any, error) {
		query := args.Query
		if query == "" {
			return nil, nil, fmt.Errorf("query is required")
		}

		var timeParam int64

		if args.TimeISO != "" {
			t, err := time.Parse(time.RFC3339, args.TimeISO)
			if err != nil {
				return nil, nil, fmt.Errorf("invalid time_iso format, must be ISO8601: %w", err)
			}
			timeParam = t.Unix()
		} else {
			timeParam = time.Now().Unix()
		}

		httpResp, err := utils.MakePromInstantAPIQuery(ctx, client, query, timeParam, cfg)
		if err != nil {
			return nil, nil, err
		}
		if httpResp == nil {
			return nil, nil, fmt.Errorf("received nil response from Prometheus")
		}
		if httpResp.StatusCode != http.StatusOK {
			return nil, nil, fmt.Errorf("failed to execute Prometheus instant query: %s", httpResp.Status)
		}
		defer httpResp.Body.Close()
		responseBodyBytes, err := io.ReadAll(httpResp.Body)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to read response body: %w", err)
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: string(responseBodyBytes),
				},
			},
		}, nil, nil
	}
}

// tool handler to get label values for a given label name and filter prometheus query
// handler for prometheus instant query
const GetServiceEnvironmentsDescription = `
	Return the environments available for the services. This tool returns an array of environments. These env can act as 
	label or argument values for other tools.
	Parameters:
	- start_time: (Optional) Start time of the time range in ISO format. Defaults to end_time - 1 hour
	- end_time: (Optional) End time of the time range in ISO format. Defaults to current time

	Returns an array of environments.
`

const GetServiceDependencyGraphDescription = `
	Get the service dependency graph showing relationships between services.
	Parameters:
	- start_time_iso: (Optional) Start time in ISO8601 format
	- end_time_iso: (Optional) End time in ISO8601 format
	- env: (Optional) Environment to filter by
	- service_name: (Optional) Service name to focus on in the dependency graph

	Returns the dependency graph data.
`

const GetPromqlRangeQueryDescription = `

	Execute a PromQL range query against the metrics data.
	Parameters:
	- query: PromQL query to execute
	- start_time_iso: (Optional) Start time in ISO8601 format
	- end_time_iso: (Optional) End time in ISO8601 format

	Returns time series data for the specified time range.


Usage guidance (from get_metrics):

# get_metrics Tool Usage Guide

Use for performance monitoring, trend analysis, and system health checks.

## When to Use
- Monitoring system performance trends
- Analyzing response time patterns
- Tracking error rates over time
- Identifying performance bottlenecks
- Comparing metrics across time periods
- Detecting anomalies and spikes

## Best Practices

### Time Range Selection
- **Recent monitoring**: Last 15-30 minutes
- **Trend analysis**: Last few hours to days
- **Performance baselines**: Week or month comparisons
- **Incident investigation**: Before, during, and after incident times

### Metric Selection
Choose metrics that align with the user's question:
- **Response times**: For performance issues
- **Error rates**: For reliability concerns
- **Throughput**: For capacity planning
- **Cache ratios**: For CDN optimization

## Common Use Cases

### Performance Analysis
- Response time trends
- Latency percentiles (p50, p95, p99)
- Throughput measurements
- Resource utilization

### Error Monitoring
- Error rate trends
- Error distribution by type
- Impact analysis

### Capacity Planning
- Traffic volume patterns
- Peak usage identification
- Growth trend analysis

## Tips
- Consider time zones when analyzing patterns
- Look for correlations between different metrics
- Use appropriate granularity for time ranges
- Compare against historical baselines when possible
`

const GetPromqlInstantQueryDescription = `

	Execute a PromQL instant query against the metrics data.
	Parameters:
	- query: PromQL query to execute
	- time_iso: (Optional) Evaluation time in ISO8601 format

	Returns instant query results.


Usage guidance (from get_metrics):

# get_metrics Tool Usage Guide

Use for performance monitoring, trend analysis, and system health checks.

## When to Use
- Monitoring system performance trends
- Analyzing response time patterns
- Tracking error rates over time
- Identifying performance bottlenecks
- Comparing metrics across time periods
- Detecting anomalies and spikes

## Best Practices

### Time Range Selection
- **Recent monitoring**: Last 15-30 minutes
- **Trend analysis**: Last few hours to days
- **Performance baselines**: Week or month comparisons
- **Incident investigation**: Before, during, and after incident times

### Metric Selection
Choose metrics that align with the user's question:
- **Response times**: For performance issues
- **Error rates**: For reliability concerns
- **Throughput**: For capacity planning
- **Cache ratios**: For CDN optimization

## Common Use Cases

### Performance Analysis
- Response time trends
- Latency percentiles (p50, p95, p99)
- Throughput measurements
- Resource utilization

### Error Monitoring
- Error rate trends
- Error distribution by type
- Impact analysis

### Capacity Planning
- Traffic volume patterns
- Peak usage identification
- Growth trend analysis

## Tips
- Consider time zones when analyzing patterns
- Look for correlations between different metrics
- Use appropriate granularity for time ranges
- Compare against historical baselines when possible
`

const GetPromqlLabelValuesDescription = `
	Get label values for a specific label name in PromQL.
	Parameters:
	- label: Label name to get values for
	- match_query: (Optional) PromQL query to match series
	- start_time_iso: (Optional) Start time in ISO8601 format
	- end_time_iso: (Optional) End time in ISO8601 format

	Returns available values for the specified label.
`

const GetPromqlLabelsDescription = `
	Get available labels from PromQL metrics.
	Parameters:
	- match_query: (Optional) PromQL query to match series
	- start_time_iso: (Optional) Start time in ISO8601 format
	- end_time_iso: (Optional) End time in ISO8601 format

	Returns available label names.
`

// tool handler to make the query
// sum by (env)(last_over_time(domain_attributes_count))
// iterate over the values of `env` label and return the unique values
func NewServiceEnvironmentsHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, ServiceEnvironmentsArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args ServiceEnvironmentsArgs) (*mcp.CallToolResult, any, error) {
		var startTimeParam, endTimeParam int64

		// Handle end_time
		if args.EndTimeISO != "" {
			t, err := time.Parse(time.RFC3339, args.EndTimeISO)
			if err != nil {
				return nil, nil, fmt.Errorf("invalid end_time_iso format, must be ISO8601: %w", err)
			}
			endTimeParam = t.Unix()
		} else {
			endTimeParam = time.Now().Unix()
		}

		// Handle start_time
		if args.StartTimeISO != "" {
			t, err := time.Parse(time.RFC3339, args.StartTimeISO)
			if err != nil {
				return nil, nil, fmt.Errorf("invalid start_time_iso format, must be ISO8601: %w", err)
			}
			startTimeParam = t.Unix()
		} else {
			startTimeParam = endTimeParam - 3600 // default to last hour
		}

		httpResp, err := utils.MakePromLabelValuesAPIQuery(ctx, client, "env", "domain_attributes_count{span_kind='SPAN_KIND_SERVER'}", startTimeParam, endTimeParam, cfg)
		if err != nil {
			return nil, nil, err
		}
		if httpResp == nil {
			return nil, nil, fmt.Errorf("received nil response from Prometheus")
		}
		if httpResp.StatusCode != http.StatusOK {
			return nil, nil, fmt.Errorf("failed to execute Prometheus label values query: %s", httpResp.Status)
		}
		defer httpResp.Body.Close()
		// Read the response body
		responseBodyBytes, err := io.ReadAll(httpResp.Body)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to read response body: %w", err)
		}

		// Return the environments as the content
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: string(responseBodyBytes),
				},
			},
		}, nil, nil
	}
}

// tool handler to get label values for a given label name and filter prometheus query
// handler for prometheus instant query
const PromqlLabelValuesQueryDetails = `
	Return the label values for a particular label and promql filter query.
	This works similar to the prometheus /label_values call
	It returns an array of values for the label.
	Parameters:
	- match_query: (Required) A valid promql filter query
	- label: (Required) Name of the label to return values for 
	- start_time: (Optional) Start time of the time range in ISO format. Defaults to end_time - 1 hour
	- end_time: (Optional) End time of the time range in ISO format. Defaults to current time

	match_query should be a well formed, valid promql query
	It is enouraged to not use default
	values of start_time and end_time and use values that are appropriate for the 
	use case
`

func NewPromqlLabelValuesHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, PromqlLabelValuesArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args PromqlLabelValuesArgs) (*mcp.CallToolResult, any, error) {
		query := args.MatchQuery
		if query == "" {
			return nil, nil, fmt.Errorf("match_query is required")
		}
		label := args.Label
		if label == "" {
			return nil, nil, fmt.Errorf("label is required")
		}
		var startTimeParam, endTimeParam int64

		// Handle end_time
		if args.EndTimeISO != "" {
			t, err := time.Parse(time.RFC3339, args.EndTimeISO)
			if err != nil {
				return nil, nil, fmt.Errorf("invalid end_time_iso format, must be ISO8601: %w", err)
			}
			endTimeParam = t.Unix()
		} else {
			endTimeParam = time.Now().Unix()
		}

		// Handle start_time
		if args.StartTimeISO != "" {
			t, err := time.Parse(time.RFC3339, args.StartTimeISO)
			if err != nil {
				return nil, nil, fmt.Errorf("invalid start_time_iso format, must be ISO8601: %w", err)
			}
			startTimeParam = t.Unix()
		} else {
			startTimeParam = endTimeParam - 3600 // default to last hour
		}

		httpResp, err := utils.MakePromLabelValuesAPIQuery(ctx, client, label, query, startTimeParam, endTimeParam, cfg)
		if err != nil {
			return nil, nil, err
		}
		if httpResp == nil {
			return nil, nil, fmt.Errorf("received nil response from Prometheus")
		}
		if httpResp.StatusCode != http.StatusOK {
			return nil, nil, fmt.Errorf("failed to execute Prometheus range query: %s", httpResp.Status)
		}
		defer httpResp.Body.Close()
		// return the response body string as the content without parsing
		responseBodyBytes, err := io.ReadAll(httpResp.Body)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to read response body: %w", err)
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: string(responseBodyBytes),
				},
			},
		}, nil, nil
	}
}

// tool handler to get label values for a given label name and filter prometheus query
// handler for prometheus instant query
const PromqlLabelsQueryDetails = `
	Return the labels for a given  promql match query.
	This works similar to the prometheus /labels call
	It returns an array of labels.
	Parameters:
	- match_query: (Required) A valid promql filter query
	- start_time: (Optional) Start time of the time range in ISO format. Defaults to end_time - 1 hour
	- end_time: (Optional) End time of the time range in ISO format. Defaults to current time

	match_query should be a well formed, valid promql query
	It is enouraged to not use default
	values of start_time and end_time and use values that are appropriate for the 
	use case
`

func NewPromqlLabelsHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, PromqlLabelsArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args PromqlLabelsArgs) (*mcp.CallToolResult, any, error) {
		query := args.MatchQuery
		if query == "" {
			return nil, nil, fmt.Errorf("match_query is required")
		}
		var startTimeParam, endTimeParam int64

		// Handle end_time
		if args.EndTimeISO != "" {
			t, err := time.Parse(time.RFC3339, args.EndTimeISO)
			if err != nil {
				return nil, nil, fmt.Errorf("invalid end_time_iso format, must be ISO8601: %w", err)
			}
			endTimeParam = t.Unix()
		} else {
			endTimeParam = time.Now().Unix()
		}

		// Handle start_time
		if args.StartTimeISO != "" {
			t, err := time.Parse(time.RFC3339, args.StartTimeISO)
			if err != nil {
				return nil, nil, fmt.Errorf("invalid start_time_iso format, must be ISO8601: %w", err)
			}
			startTimeParam = t.Unix()
		} else {
			startTimeParam = endTimeParam - 3600 // default to last hour
		}

		httpResp, err := utils.MakePromLabelsAPIQuery(ctx, client, query, startTimeParam, endTimeParam, cfg)
		if err != nil {
			return nil, nil, err
		}
		if httpResp == nil {
			return nil, nil, fmt.Errorf("received nil response from Prometheus")
		}
		if httpResp.StatusCode != http.StatusOK {
			return nil, nil, fmt.Errorf("failed to execute Prometheus range query: %s", httpResp.Status)
		}
		defer httpResp.Body.Close()
		// return the response body string as the content without parsing
		responseBodyBytes, err := io.ReadAll(httpResp.Body)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to read response body: %w", err)
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: string(responseBodyBytes),
				},
			},
		}, nil, nil
	}
}
