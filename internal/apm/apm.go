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

// firstNonEmpty returns the first non-empty string, enabling canonical-wins
// resolution between a canonical param and its alias.
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
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
type ServiceEnvironmentsArgs struct {
	StartTimeISO    string  `json:"start_time_iso,omitempty" jsonschema:"Start time in RFC3339/ISO8601 format (e.g. 2024-06-01T12:00:00Z). Optional when lookback_minutes is provided."`
	EndTimeISO      string  `json:"end_time_iso,omitempty" jsonschema:"End time in RFC3339/ISO8601 format (e.g. 2024-06-01T13:00:00Z). Defaults to now when omitted."`
	LookbackMinutes float64 `json:"lookback_minutes,omitempty" jsonschema:"Number of minutes to look back from now (default: 60, minimum: 1). Use for relative windows like last 30 minutes."`
	ServiceName     string  `json:"service_name,omitempty" jsonschema:"Optional service name to filter environments for (e.g. my-api). When omitted, returns environments across all services."`
}

type ServicePerformanceDetailsArgs struct {
	ServiceName     string  `json:"service_name" jsonschema:"Name of the service to get performance details for (required)"`
	StartTimeISO    string  `json:"start_time_iso,omitempty" jsonschema:"Start time in RFC3339/ISO8601 format (e.g. 2024-06-01T12:00:00Z). Optional when lookback_minutes is provided."`
	EndTimeISO      string  `json:"end_time_iso,omitempty" jsonschema:"End time in RFC3339/ISO8601 format (e.g. 2024-06-01T13:00:00Z). Defaults to now when omitted."`
	LookbackMinutes float64 `json:"lookback_minutes,omitempty" jsonschema:"Number of minutes to look back from now (default: 60, minimum: 1). Use for relative windows like last 30 minutes."`
	Env             string  `json:"env,omitempty" jsonschema:"Environment to filter by (default: .*, e.g. prod)"`
	TopN            int     `json:"top_n,omitempty" jsonschema:"Max entries to return for top_operations_by_response_time, top_operations_by_error_rate, and top_errors (default: 10). Values above 100 clamp to 100."`
}

type ServiceOperationsSummaryArgs struct {
	ServiceName     string  `json:"service_name" jsonschema:"Name of the service to get operations summary for (required)"`
	StartTimeISO    string  `json:"start_time_iso,omitempty" jsonschema:"Start time in RFC3339/ISO8601 format (e.g. 2024-06-01T12:00:00Z). Optional when lookback_minutes is provided."`
	EndTimeISO      string  `json:"end_time_iso,omitempty" jsonschema:"End time in RFC3339/ISO8601 format (e.g. 2024-06-01T13:00:00Z). Defaults to now when omitted."`
	LookbackMinutes float64 `json:"lookback_minutes,omitempty" jsonschema:"Number of minutes to look back from now (default: 60, minimum: 1). Use for relative windows like last 30 minutes."`
	Env             string  `json:"env,omitempty" jsonschema:"Environment to filter by (default: .*, e.g. prod)"`
}

type ServiceDependencyGraphArgs struct {
	StartTimeISO    string  `json:"start_time_iso,omitempty" jsonschema:"Start time in RFC3339/ISO8601 format (e.g. 2024-06-01T12:00:00Z). Optional when lookback_minutes is provided."`
	EndTimeISO      string  `json:"end_time_iso,omitempty" jsonschema:"End time in RFC3339/ISO8601 format (e.g. 2024-06-01T13:00:00Z). Defaults to now when omitted."`
	LookbackMinutes float64 `json:"lookback_minutes,omitempty" jsonschema:"Number of minutes to look back from now (default: 60, minimum: 1). Use for relative windows like last 30 minutes."`
	Env             string  `json:"env,omitempty" jsonschema:"Environment to filter by (default: .*, e.g. prod)"`
	ServiceName     string  `json:"service_name,omitempty" jsonschema:"Service name to focus on in the dependency graph (e.g. api-service)"`
}

type PromqlRangeQueryArgs struct {
	Query           string  `json:"query" jsonschema:"PromQL query to execute (required)"`
	StartTimeISO    string  `json:"start_time_iso,omitempty" jsonschema:"Start time in RFC3339/ISO8601 format (e.g. 2024-06-01T12:00:00Z). Optional when lookback_minutes is provided."`
	EndTimeISO      string  `json:"end_time_iso,omitempty" jsonschema:"End time in RFC3339/ISO8601 format (e.g. 2024-06-01T13:00:00Z). Defaults to now when omitted."`
	LookbackMinutes float64 `json:"lookback_minutes,omitempty" jsonschema:"Number of minutes to look back from now (default: 60, minimum: 1). Use for relative windows like last 30 minutes."`
	Datasource      string  `json:"datasource,omitempty" jsonschema:"Name of the datasource to query. If omitted, uses the default configured datasource."`
}

type PromqlInstantQueryArgs struct {
	Query           string  `json:"query" jsonschema:"PromQL query to execute (required)"`
	TimeISO         string  `json:"time_iso,omitempty" jsonschema:"Evaluation time in RFC3339/ISO8601 format (e.g. 2024-06-01T12:00:00Z). If omitted, defaults to now or now-lookback_minutes."`
	LookbackMinutes float64 `json:"lookback_minutes,omitempty" jsonschema:"Number of minutes to look back from now when time_iso is omitted (default: 0, minimum: 1)."`
	Datasource      string  `json:"datasource,omitempty" jsonschema:"Name of the datasource to query. If omitted, uses the default configured datasource."`
}

type PromqlLabelValuesArgs struct {
	MatchQuery      string  `json:"match_query,omitempty" jsonschema:"PromQL query to match series (e.g. up{job=\"prometheus\"})"`
	Match           string  `json:"match,omitempty" jsonschema:"Alias of match_query (matches the Prometheus API's match parameter); ignored when match_query is set."`
	Label           string  `json:"label" jsonschema:"Label name to get values for (required)"`
	StartTimeISO    string  `json:"start_time_iso,omitempty" jsonschema:"Start time in RFC3339/ISO8601 format (e.g. 2024-06-01T12:00:00Z). Optional when lookback_minutes is provided."`
	EndTimeISO      string  `json:"end_time_iso,omitempty" jsonschema:"End time in RFC3339/ISO8601 format (e.g. 2024-06-01T13:00:00Z). Defaults to now when omitted."`
	LookbackMinutes float64 `json:"lookback_minutes,omitempty" jsonschema:"Number of minutes to look back from now (default: 60, minimum: 1). Use for relative windows like last 30 minutes."`
	Datasource      string  `json:"datasource,omitempty" jsonschema:"Name of the datasource to query. If omitted, uses the default configured datasource."`
}

type PromqlLabelsArgs struct {
	MatchQuery      string  `json:"match_query,omitempty" jsonschema:"PromQL query to match series (e.g. up{job=\"prometheus\"})"`
	Match           string  `json:"match,omitempty" jsonschema:"Alias of match_query (matches the Prometheus API's match parameter); ignored when match_query is set."`
	StartTimeISO    string  `json:"start_time_iso,omitempty" jsonschema:"Start time in RFC3339/ISO8601 format (e.g. 2024-06-01T12:00:00Z). Optional when lookback_minutes is provided."`
	EndTimeISO      string  `json:"end_time_iso,omitempty" jsonschema:"End time in RFC3339/ISO8601 format (e.g. 2024-06-01T13:00:00Z). Defaults to now when omitted."`
	LookbackMinutes float64 `json:"lookback_minutes,omitempty" jsonschema:"Number of minutes to look back from now (default: 60, minimum: 1). Use for relative windows like last 30 minutes."`
	Datasource      string  `json:"datasource,omitempty" jsonschema:"Name of the datasource to query. If omitted, uses the default configured datasource."`
}

func resolveTimeRange(startTimeISO, endTimeISO string, lookbackMinutes float64) (int64, int64, error) {
	params := map[string]interface{}{}
	if startTimeISO != "" {
		params["start_time_iso"] = startTimeISO
	}
	if endTimeISO != "" {
		params["end_time_iso"] = endTimeISO
	}
	if lookbackMinutes != 0 {
		params["lookback_minutes"] = lookbackMinutes
	}

	startTime, endTime, err := utils.GetTimeRange(params, utils.DefaultLookbackMinutes)
	if err != nil {
		return 0, 0, err
	}

	return startTime.Unix(), endTime.Unix(), nil
}

func resolveInstantQueryTime(timeISO string, lookbackMinutes float64) (int64, error) {
	if timeISO != "" {
		_, endTime, err := utils.GetTimeRange(map[string]interface{}{
			"end_time_iso": timeISO,
		}, utils.DefaultLookbackMinutes)
		if err != nil {
			return 0, fmt.Errorf("invalid time_iso format: %w", err)
		}
		return endTime.Unix(), nil
	}

	if lookbackMinutes != 0 {
		startTime, _, err := utils.GetTimeRange(map[string]interface{}{
			"lookback_minutes": lookbackMinutes,
		}, utils.DefaultLookbackMinutes)
		if err != nil {
			return 0, err
		}
		return startTime.Unix(), nil
	}

	return time.Now().UTC().Unix(), nil
}

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

// resolveDatasourceCfg returns a copy of cfg with Prometheus credentials overridden
// to those of the named datasource. If datasourceName is empty the original cfg is
// returned unchanged. Returns an error when the name is non-empty but not found.
func resolveDatasourceCfg(cfg models.Config, datasourceName string) (models.Config, error) {
	if datasourceName == "" {
		return cfg, nil
	}
	ds, ok := cfg.ResolveDatasource(datasourceName)
	if !ok {
		return cfg, fmt.Errorf("datasource %q not found", datasourceName)
	}
	if ds.ReadURL == "" || ds.Username == "" || ds.Password == "" || ds.Region == "" {
		return cfg, fmt.Errorf("datasource %q is missing required Prometheus configuration", datasourceName)
	}
	cfg.PrometheusReadURL = ds.ReadURL
	cfg.PrometheusUsername = ds.Username
	cfg.PrometheusPassword = ds.Password
	cfg.Region = ds.Region
	cfg.ClusterID = ds.ClusterID
	return cfg, nil
}

func promToolError(resp *http.Response, op string) (*mcp.CallToolResult, any, error) {
	err := utils.NewUpstreamHTTPError(resp, op)
	return utils.ToolErrorResult(err.Error()), nil, nil
}

func promErr(resp *http.Response, op string) error {
	return utils.NewUpstreamHTTPError(resp, op)
}

func NewPromqlRangeQueryHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, PromqlRangeQueryArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args PromqlRangeQueryArgs) (*mcp.CallToolResult, any, error) {
		query := args.Query
		if query == "" {
			return nil, nil, fmt.Errorf("query is required")
		}

		startTimeParam, endTimeParam, err := resolveTimeRange(args.StartTimeISO, args.EndTimeISO, args.LookbackMinutes)
		if err != nil {
			return nil, nil, err
		}

		queryCfg, err := resolveDatasourceCfg(cfg, args.Datasource)
		if err != nil {
			return nil, nil, err
		}

		httpResp, err := utils.MakePromRangeAPIQuery(ctx, client, query, startTimeParam, endTimeParam, queryCfg, utils.PromResolution{})
		if err != nil {
			return nil, nil, err
		}
		if httpResp == nil {
			return nil, nil, fmt.Errorf("received nil response from Prometheus")
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode != http.StatusOK {
			return promToolError(httpResp, "Prometheus range query")
		}
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

func NewPromqlInstantQueryHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, PromqlInstantQueryArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args PromqlInstantQueryArgs) (*mcp.CallToolResult, any, error) {
		query := args.Query
		if query == "" {
			return nil, nil, fmt.Errorf("query is required")
		}

		timeParam, err := resolveInstantQueryTime(args.TimeISO, args.LookbackMinutes)
		if err != nil {
			return nil, nil, err
		}

		queryCfg, err := resolveDatasourceCfg(cfg, args.Datasource)
		if err != nil {
			return nil, nil, err
		}

		httpResp, err := utils.MakePromInstantAPIQuery(ctx, client, query, timeParam, queryCfg)
		if err != nil {
			return nil, nil, err
		}
		if httpResp == nil {
			return nil, nil, fmt.Errorf("received nil response from Prometheus")
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode != http.StatusOK {
			return promToolError(httpResp, "Prometheus instant query")
		}
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

// tool handler to make the query
// sum by (env)(last_over_time(domain_attributes_count))
// iterate over the values of `env` label and return the unique values
func NewServiceEnvironmentsHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, ServiceEnvironmentsArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args ServiceEnvironmentsArgs) (*mcp.CallToolResult, any, error) {
		startTimeParam, endTimeParam, err := resolveTimeRange(args.StartTimeISO, args.EndTimeISO, args.LookbackMinutes)
		if err != nil {
			return nil, nil, err
		}

		var matchQuery string
		if args.ServiceName != "" {
			matchQuery = fmt.Sprintf("domain_attributes_count{span_kind='SPAN_KIND_SERVER',service_name=%q}", args.ServiceName)
		} else {
			matchQuery = "domain_attributes_count{span_kind='SPAN_KIND_SERVER'}"
		}
		httpResp, err := utils.MakePromLabelValuesAPIQuery(ctx, client, "env", matchQuery, startTimeParam, endTimeParam, cfg)
		if err != nil {
			return nil, nil, err
		}
		if httpResp == nil {
			return nil, nil, fmt.Errorf("received nil response from Prometheus")
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode != http.StatusOK {
			return promToolError(httpResp, "Prometheus label values")
		}
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

func NewPromqlLabelValuesHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, PromqlLabelValuesArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args PromqlLabelValuesArgs) (*mcp.CallToolResult, any, error) {
		query := firstNonEmpty(args.MatchQuery, args.Match)
		if query == "" {
			return nil, nil, fmt.Errorf("match_query is required")
		}
		label := args.Label
		if label == "" {
			return nil, nil, fmt.Errorf("label is required")
		}
		startTimeParam, endTimeParam, err := resolveTimeRange(args.StartTimeISO, args.EndTimeISO, args.LookbackMinutes)
		if err != nil {
			return nil, nil, err
		}

		queryCfg, err := resolveDatasourceCfg(cfg, args.Datasource)
		if err != nil {
			return nil, nil, err
		}

		httpResp, err := utils.MakePromLabelValuesAPIQuery(ctx, client, label, query, startTimeParam, endTimeParam, queryCfg)
		if err != nil {
			return nil, nil, err
		}
		if httpResp == nil {
			return nil, nil, fmt.Errorf("received nil response from Prometheus")
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode != http.StatusOK {
			return promToolError(httpResp, "Prometheus label values")
		}
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

func NewPromqlLabelsHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, PromqlLabelsArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args PromqlLabelsArgs) (*mcp.CallToolResult, any, error) {
		query := firstNonEmpty(args.MatchQuery, args.Match)
		if query == "" {
			return nil, nil, fmt.Errorf("match_query is required")
		}
		startTimeParam, endTimeParam, err := resolveTimeRange(args.StartTimeISO, args.EndTimeISO, args.LookbackMinutes)
		if err != nil {
			return nil, nil, err
		}

		queryCfg, err := resolveDatasourceCfg(cfg, args.Datasource)
		if err != nil {
			return nil, nil, err
		}

		httpResp, err := utils.MakePromLabelsAPIQuery(ctx, client, query, startTimeParam, endTimeParam, queryCfg)
		if err != nil {
			return nil, nil, err
		}
		if httpResp == nil {
			return nil, nil, fmt.Errorf("received nil response from Prometheus")
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode != http.StatusOK {
			return promToolError(httpResp, "Prometheus labels")
		}
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

// ListDatasourcesArgs has no required parameters.
type ListDatasourcesArgs struct{}

// NewListDatasourcesHandler returns a handler that serves the datasource list from
// the in-memory cache populated at startup — no extra API call is made.
// The response is serialized once at registration time since the list never changes.
func NewListDatasourcesHandler(cfg models.Config) func(context.Context, *mcp.CallToolRequest, ListDatasourcesArgs) (*mcp.CallToolResult, any, error) {
	type datasourceView struct {
		Name      string `json:"name"`
		IsDefault bool   `json:"is_default"`
	}

	views := make([]datasourceView, 0, len(cfg.Datasources))
	for _, ds := range cfg.Datasources {
		views = append(views, datasourceView{
			Name:      ds.Name,
			IsDefault: ds.IsDefault,
		})
	}
	out, _ := json.Marshal(views) // slice of plain structs — cannot fail

	return func(_ context.Context, _ *mcp.CallToolRequest, _ ListDatasourcesArgs) (*mcp.CallToolResult, any, error) {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: string(out)},
			},
		}, nil, nil
	}
}
