package change_events

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"last9-mcp/internal/deeplink"
	"last9-mcp/internal/last9api"
	"last9-mcp/internal/last9api/prom"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TimeSeriesPoint represents a single data point in a time series
type TimeSeriesPoint struct {
	Timestamp float64 `json:"timestamp"`
	Value     float64 `json:"value"`
}

// TimeSeries represents a time series with metric labels and values
type TimeSeries struct {
	Metric map[string]string `json:"metric"`
	Values []TimeSeriesPoint `json:"values"`
}

type apiPromRangeResp []struct {
	Metric map[string]string `json:"metric"`
	Values [][]any           `json:"values"`
}

// GetChangeEventsArgs represents the input arguments for the get_change_events tool
type GetChangeEventsArgs struct {
	StartTimeISO    string `json:"start_time_iso,omitempty" jsonschema:"Start time in RFC3339/ISO8601 format (e.g. 2026-02-09T15:04:05Z)"`
	EndTimeISO      string `json:"end_time_iso,omitempty" jsonschema:"End time in RFC3339/ISO8601 format (e.g. 2026-02-09T16:04:05Z)"`
	LookbackMinutes int    `json:"lookback_minutes,omitempty" jsonschema:"Number of minutes to look back from now (default: 60, minimum: 1)"`
	ServiceName     string `json:"service_name,omitempty" jsonschema:"Service name filter (optional)"`
	Env             string `json:"env,omitempty" jsonschema:"Environment filter (optional)"`
	EventName       string `json:"event_name,omitempty" jsonschema:"Exact event name filter (optional). Use available_event_names from a previous call."`
}

func NewGetChangeEventsHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, GetChangeEventsArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args GetChangeEventsArgs) (*mcp.CallToolResult, any, error) {
		timeParams := map[string]interface{}{}
		if args.LookbackMinutes > 0 {
			timeParams["lookback_minutes"] = args.LookbackMinutes
		}
		if args.StartTimeISO != "" {
			timeParams["start_time_iso"] = args.StartTimeISO
		}
		if args.EndTimeISO != "" {
			timeParams["end_time_iso"] = args.EndTimeISO
		}

		startTime, endTime, err := utils.GetTimeRange(timeParams, utils.DefaultLookbackMinutes)
		if err != nil {
			return nil, nil, err
		}
		startTimeParam := startTime.Unix()
		endTimeParam := endTime.Unix()

		api := last9api.NewClient(client, cfg)
		datasource := prom.Datasource{ReadURL: cfg.PrometheusReadURL, Username: cfg.PrometheusUsername, Password: cfg.PrometheusPassword}
		// The instant transport uses whole seconds. Cover both endpoints, then
		// discard any fringe samples outside the caller's precise bounds.
		evaluation := endTimeParam
		if endTime.Nanosecond() != 0 {
			evaluation++
		}
		queryEvents := func(selector string) ([]TimeSeries, error) {
			raw, err := prom.QueryInstantRaw(ctx, api, prom.QueryInstantInput{
				Datasource: datasource, Query: fmt.Sprintf("%s[%ds]", selector, evaluation-startTimeParam+1), Timestamp: evaluation,
			})
			if err != nil {
				return nil, fmt.Errorf("failed to query change events: %w", err)
			}
			series, err := parseChangeEventsTimeSeries(raw, startTime, endTime)
			if err != nil {
				return nil, fmt.Errorf("failed to parse change events: %w", err)
			}
			return series, nil
		}

		selectors := changeEventSelectors(args)
		changeEvents := []TimeSeries{}
		for _, selector := range selectors {
			series, err := queryEvents(selector)
			if err != nil {
				return nil, nil, err
			}
			changeEvents = append(changeEvents, series...)
		}
		availableEventNames := eventNames(changeEvents)

		result := map[string]any{
			"available_event_names": availableEventNames,
			"change_events":         changeEvents,
			"count":                 len(changeEvents),
			"time_range": map[string]any{
				"start": startTime.Format(time.RFC3339Nano),
				"end":   endTime.Format(time.RFC3339Nano),
			},
		}

		// Format the response as JSON
		resultJSON, err := json.Marshal(result)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to marshal result: %w", err)
		}

		// The dashboard has no page that lists change events for a time range
		// (changeboards need a board id), so link the exact query in the
		// Metrics explorer: it lists each event series with its labels.
		dashboardURL := deeplink.NewBuilder(cfg.OrgSlug, cfg.ClusterID).BuildMetricsLink(startTimeParam*1000, endTimeParam*1000, strings.Join(selectors, " or "))
		return &mcp.CallToolResult{
			Meta: deeplink.ToMeta(dashboardURL),
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: string(resultJSON),
				},
			},
		}, nil, nil
	}
}

func eventNames(events []TimeSeries) []string {
	names := make(map[string]struct{})
	for _, event := range events {
		if name := event.Metric["event_name"]; name != "" {
			names[name] = struct{}{}
		}
	}
	result := make([]string, 0, len(names))
	for name := range names {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}

const (
	changeEventsMetric           = "last9_change_events"
	excludeBackupEvents          = `event_name!~"cold_storage_logs_backup|cold_storage_logs_backup_endtime|cold_storage_logs_backup_time_taken_in_sec|manual_rehydration_event"`
	excludeScheduledSearchEvents = `l9_event_name!~"last9_scheduled_search"`
)

func changeEventSelectors(args GetChangeEventsArgs) []string {
	matchers := []string{excludeBackupEvents, excludeScheduledSearchEvents}
	matchers = addFilter(matchers, "deployment_environment", args.Env)
	matchers = addFilter(matchers, "event_name", args.EventName)
	base := changeEventsMetric + "{" + strings.Join(matchers, ",")
	if args.ServiceName == "" {
		return []string{base + "}"}
	}
	quoted := `"` + utils.EscapePromQLLabel(args.ServiceName) + `"`
	return []string{base + ",service_name=" + quoted + "}", base + `,service_name="",service=` + quoted + "}"}
}

func addFilter(matchers []string, label, value string) []string {
	if value == "" {
		return matchers
	}
	return append(matchers, fmt.Sprintf(`%s="%s"`, label, utils.EscapePromQLLabel(value)))
}

// parseChangeEventsTimeSeries converts Prometheus response to TimeSeries format
func parseChangeEventsTimeSeries(respBody []byte, start, end time.Time) ([]TimeSeries, error) {
	var promResp apiPromRangeResp
	var resp []TimeSeries

	if err := json.Unmarshal(respBody, &promResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal Prometheus response: %w", err)
	}

	if promResp == nil {
		return nil, fmt.Errorf("missing sample matrix")
	}
	for _, r := range promResp {
		series := TimeSeries{
			Metric: r.Metric,
			Values: make([]TimeSeriesPoint, 0, len(r.Values)),
		}

		if r.Values == nil {
			return nil, fmt.Errorf("expected a matrix with sample values")
		}
		for _, v := range r.Values {
			if len(v) != 2 {
				return nil, fmt.Errorf("invalid value format in Prometheus response: %v", v)
			}

			if ts, ok := v[0].(float64); ok {
				sampleTime := time.UnixMilli(int64(math.Round(ts * 1000)))
				if sampleTime.Before(start) || sampleTime.After(end) {
					continue
				}
				if valStr, ok := v[1].(string); ok {
					val, err := strconv.ParseFloat(valStr, 64)
					if err != nil {
						return nil, fmt.Errorf("failed to parse value: %w", err)
					}

					point := TimeSeriesPoint{
						Timestamp: ts,
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
		if len(series.Values) > 0 {
			resp = append(resp, series)
		}
	}

	return resp, nil
}
