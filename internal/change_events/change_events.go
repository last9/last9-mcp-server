package change_events

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TimeSeriesPoint represents a single data point in a time series
type TimeSeriesPoint struct {
	Timestamp uint64  `json:"timestamp"`
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
	EventName       string `json:"event_name,omitempty" jsonschema:"Exact event type filter (optional). Use available_event_names from a previous call."`
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

		// First, fetch all available event_name values using the series API
		availableEventNames, err := fetchAvailableEventNames(ctx, client, startTimeParam, endTimeParam, cfg)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to fetch available event names: %w", err)
		}

		promql := buildChangeEventsQuery(args)

		// Make range query to get change events over time
		resp, err := utils.MakePromRangeAPIQuery(ctx, client, promql, startTimeParam, endTimeParam, cfg)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to query change events: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return nil, nil, fmt.Errorf("change events API request failed with status %d: %s", resp.StatusCode, string(body))
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to read response body: %w", err)
		}

		// Parse Prometheus response into timeseries format
		changeEvents, err := parseChangeEventsTimeSeries(body)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to parse change events: %w", err)
		}

		result := map[string]any{
			"available_event_names": availableEventNames,
			"change_events":         changeEvents,
			"count":                 countChangeEventPoints(changeEvents),
			"series_count":          len(changeEvents),
			"time_range": map[string]any{
				"start": startTime.Format(time.RFC3339),
				"end":   endTime.Format(time.RFC3339),
			},
		}

		// Format the response as JSON
		resultJSON, err := json.Marshal(result)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to marshal result: %w", err)
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

func buildChangeEventsQuery(args GetChangeEventsArgs) string {
	baseMatchers := changeEventBaseMatchers(args)
	if args.EventName == "" {
		return changeEventSelector(baseMatchers)
	}

	eventName := strconv.Quote(args.EventName)
	selectors := []string{
		changeEventSelector(appendMatcher(baseMatchers, "event_name="+eventName)),
		changeEventSelector(appendMatchers(baseMatchers, "event_name=\"\"", "event_type="+eventName)),
		changeEventSelector(appendMatchers(baseMatchers, "event_name=\"\"", "event_type=\"\"", "l9_event_name="+eventName)),
	}
	return strings.Join(selectors, " or ")
}

func changeEventBaseMatchers(args GetChangeEventsArgs) []string {
	matchers := []string{
		`event_name!~"cold_storage_logs_backup|cold_storage_logs_backup_endtime|cold_storage_logs_backup_time_taken_in_sec|manual_rehydration_event"`,
		`l9_event_name!~"last9_scheduled_search"`,
	}
	if args.ServiceName != "" {
		matchers = append(matchers, "service_name="+strconv.Quote(args.ServiceName))
	}
	if args.Env != "" {
		matchers = append(matchers, "env="+strconv.Quote(args.Env))
	}
	return matchers
}

func appendMatcher(matchers []string, matcher string) []string {
	appended := append([]string{}, matchers...)
	return append(appended, matcher)
}

func appendMatchers(matchers []string, additions ...string) []string {
	appended := append([]string{}, matchers...)
	return append(appended, additions...)
}

func changeEventSelector(matchers []string) string {
	return "last9_change_events{" + strings.Join(matchers, ",") + "}"
}

func countChangeEventPoints(series []TimeSeries) int {
	count := 0
	for _, item := range series {
		count += len(item.Values)
	}
	return count
}

// fetchAvailableEventNames fetches all available event_name values from the last9_change_events metric
func fetchAvailableEventNames(ctx context.Context, client *http.Client, startTime, endTime int64, cfg models.Config) ([]string, error) {
	labels := []string{"event_name", "event_type", "l9_event_name"}
	uniqueNames := make(map[string]struct{})
	for _, label := range labels {
		names, err := fetchEventNamesForLabel(ctx, client, label, startTime, endTime, cfg)
		if err != nil {
			return nil, err
		}
		for _, name := range names {
			uniqueNames[name] = struct{}{}
		}
	}
	eventNames := make([]string, 0, len(uniqueNames))
	for name := range uniqueNames {
		eventNames = append(eventNames, name)
	}
	sort.Strings(eventNames)
	return eventNames, nil
}

func fetchEventNamesForLabel(
	ctx context.Context,
	client *http.Client,
	label string,
	startTime int64,
	endTime int64,
	cfg models.Config,
) ([]string, error) {
	resp, err := utils.MakePromLabelValuesAPIQuery(ctx, client, label, "last9_change_events", startTime, endTime, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to query event names: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get event names: %s", resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var eventNamesResp []string
	if err := json.Unmarshal(body, &eventNamesResp); err != nil {
		return nil, fmt.Errorf("failed to parse event names response: %w", err)
	}

	return eventNamesResp, nil
}

// parseChangeEventsTimeSeries converts Prometheus response to TimeSeries format
func parseChangeEventsTimeSeries(respBody []byte) ([]TimeSeries, error) {
	var promResp apiPromRangeResp
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
