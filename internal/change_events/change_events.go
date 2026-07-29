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
	"sync"
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

var excludedEventNames = map[string]struct{}{
	"cold_storage_logs_backup":                   {},
	"cold_storage_logs_backup_endtime":           {},
	"cold_storage_logs_backup_time_taken_in_sec": {},
	"last9_scheduled_search":                     {},
	"manual_rehydration_event":                   {},
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

		availableEventNames, discoveryWarnings := fetchAvailableEventNames(
			ctx, client, startTimeParam, endTimeParam, cfg,
		)

		promql := buildChangeEventsQuery(args, endTimeParam-startTimeParam)

		// Query a range vector at the requested end time. A Prometheus range query
		// evaluates a sparse gauge repeatedly and can duplicate one recorded event
		// at every step because of staleness lookback.
		resp, err := utils.MakePromInstantAPIQuery(ctx, client, promql, endTimeParam, cfg)
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
		changeEvents = filterChangeEventSeries(changeEvents, args.EventName)

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
		if len(discoveryWarnings) > 0 {
			result["warnings"] = discoveryWarnings
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

func buildChangeEventsQuery(args GetChangeEventsArgs, windowSeconds int64) string {
	return fmt.Sprintf("%s[%ds]", changeEventSelector(changeEventBaseMatchers(args)), windowSeconds)
}

func changeEventBaseMatchers(args GetChangeEventsArgs) []string {
	matchers := make([]string, 0, 4)
	if args.EventName == "" {
		matchers = append(matchers,
			`event_name!~"cold_storage_logs_backup|cold_storage_logs_backup_endtime|cold_storage_logs_backup_time_taken_in_sec|manual_rehydration_event"`,
			`l9_event_name!~"last9_scheduled_search"`,
		)
	}
	if args.ServiceName != "" {
		matchers = append(matchers, "service_name="+strconv.Quote(args.ServiceName))
	}
	if args.Env != "" {
		matchers = append(matchers, "env="+strconv.Quote(args.Env))
	}
	return matchers
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

func filterChangeEventSeries(series []TimeSeries, requestedEventName string) []TimeSeries {
	filtered := make([]TimeSeries, 0, len(series))
	for _, item := range series {
		eventName := canonicalEventName(item.Metric)
		if requestedEventName != "" && eventName == requestedEventName {
			filtered = append(filtered, item)
			continue
		}
		if requestedEventName == "" {
			if _, excluded := excludedEventNames[eventName]; excluded {
				continue
			}
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func canonicalEventName(labels map[string]string) string {
	for _, label := range []string{"event_name", "event_type", "l9_event_name"} {
		if value := labels[label]; value != "" {
			return value
		}
	}
	return ""
}

// fetchAvailableEventNames fetches all available event_name values from the last9_change_events metric
func fetchAvailableEventNames(
	ctx context.Context,
	client *http.Client,
	startTime int64,
	endTime int64,
	cfg models.Config,
) ([]string, []string) {
	labels := []string{"event_name", "event_type", "l9_event_name"}
	type labelResult struct {
		names []string
		err   error
	}
	results := make([]labelResult, len(labels))
	var waitGroup sync.WaitGroup
	waitGroup.Add(len(labels))
	for index, label := range labels {
		go func() {
			defer waitGroup.Done()
			results[index].names, results[index].err = fetchEventNamesForLabel(
				ctx, client, label, startTime, endTime, cfg,
			)
		}()
	}
	waitGroup.Wait()

	uniqueNames := make(map[string]struct{})
	warnings := make([]string, 0, len(labels))
	for index, result := range results {
		if result.err != nil {
			warnings = append(warnings, fmt.Sprintf("event-name discovery for %s was unavailable", labels[index]))
			continue
		}
		for _, name := range result.names {
			if _, excluded := excludedEventNames[name]; excluded || name == "" {
				continue
			}
			uniqueNames[name] = struct{}{}
		}
	}
	eventNames := make([]string, 0, len(uniqueNames))
	for name := range uniqueNames {
		eventNames = append(eventNames, name)
	}
	sort.Strings(eventNames)
	return eventNames, warnings
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
