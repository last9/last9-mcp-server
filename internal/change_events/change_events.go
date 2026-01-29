package change_events

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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

const GetChangeEventsDescription = `Get change events from the last9_change_events prometheus metric over a given time range.
Returns change events that occurred in the specified time window.
Change events include deployments, configuration changes, and other system modifications.

The response includes:
- available_event_names: List of all available event types that can be used for filtering
- change_events: Array of timeseries data with metric labels and timestamp-value pairs
- count: Total number of change events returned
- time_range: Start and end time of the query window

Each change event includes:
- metric: Map of metric labels (service_name, env, event_type, message, etc.)
- values: Array of timestamp-value pairs representing the timeseries data

For optimal results, first call without event_name to get available_event_names, then use the exact event name from available_event_names for the event_name parameter. This approach is more reliable and eliminates ambiguity in event type detection.

Common event types (check available_event_names for actual values):
- deployment: deployment events, releases, builds, rollouts
- config_change: configuration changes, settings updates, parameter changes
- rollback: rollback events, reverts, undo operations
- scale_up/scale_down: scaling operations, capacity changes
- restart: service restarts, reboots, reloads
- upgrade/downgrade: version changes, updates
- maintenance: maintenance windows, scheduled downtime
- backup/restore: backup operations, recovery
- health_check: health checks, monitoring, status probes
- certificate: SSL/TLS operations, renewals, expirations
- database: database changes, migrations, schema updates

Best practices:
1. First call without event_name to get available_event_names
2. Use exact event name from available_event_names for the event_name parameter
3. Combine with other filters (service, environment, time) for precise results
4. Use available_event_names to discover what event types are available in the system

Parameters:
- start_time_iso: (Optional) Start time in ISO format (YYYY-MM-DD HH:MM:SS). Defaults to now - lookback_minutes.
- end_time_iso: (Optional) End time in ISO format (YYYY-MM-DD HH:MM:SS). Defaults to now.
- lookback_minutes: (Optional) Number of minutes to look back from now. Defaults to 60 minutes.
- service: (Optional) Name of the service to filter change events for
- environment: (Optional) Environment to filter by
- event_name: (Optional) Name of the change event to filter by (use available_event_names to see valid values)`

// GetChangeEventsArgs represents the input arguments for the get_change_events tool
type GetChangeEventsArgs struct {
	StartTimeISO    string `json:"start_time_iso,omitempty"`
	EndTimeISO      string `json:"end_time_iso,omitempty"`
	LookbackMinutes int    `json:"lookback_minutes,omitempty"`
	Service         string `json:"service,omitempty"`
	Environment     string `json:"environment,omitempty"`
	EventName       string `json:"event_name,omitempty"`
}

func NewGetChangeEventsHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, GetChangeEventsArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args GetChangeEventsArgs) (*mcp.CallToolResult, any, error) {
		var (
			startTimeParam, endTimeParam int64
			lookbackMinutes              = 60 // default lookback
		)

		// Handle lookback_minutes parameter
		if args.LookbackMinutes > 0 {
			lookbackMinutes = args.LookbackMinutes
		}

		// Handle end_time_iso parameter
		if args.EndTimeISO != "" {
			t, err := time.Parse("2006-01-02 15:04:05", args.EndTimeISO)
			if err != nil {
				return nil, nil, fmt.Errorf("invalid end_time_iso format: %w", err)
			}
			endTimeParam = t.Unix()
		} else {
			endTimeParam = time.Now().Unix()
		}

		// Handle start_time_iso parameter
		if args.StartTimeISO != "" {
			t, err := time.Parse("2006-01-02 15:04:05", args.StartTimeISO)
			if err != nil {
				return nil, nil, fmt.Errorf("invalid start_time_iso format: %w", err)
			}
			startTimeParam = t.Unix()
		} else {
			startTimeParam = endTimeParam - int64(lookbackMinutes*60)
		}

		// First, fetch all available event_name values using the series API
		availableEventNames, err := fetchAvailableEventNames(ctx, client, startTimeParam, endTimeParam, cfg)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to fetch available event names: %w", err)
		}

		// Build label filters for the Prometheus query
		var labelFilters []string

		if args.Service != "" {
			labelFilters = append(labelFilters, fmt.Sprintf(`service_name="%s"`, args.Service))
		}

		if args.Environment != "" {
			labelFilters = append(labelFilters, fmt.Sprintf(`env="%s"`, args.Environment))
		}

		// Use event_name parameter directly - the AI should provide the exact event type
		if args.EventName != "" {
			labelFilters = append(labelFilters, fmt.Sprintf(`event_type="%s"`, args.EventName))
		}

		// Add default filters to exclude backup and rehydration events
		labelFilters = append(labelFilters, `event_name!~"cold_storage_logs_backup|cold_storage_logs_backup_endtime|cold_storage_logs_backup_time_taken_in_sec|manual_rehydration_event"`)
		labelFilters = append(labelFilters, `l9_event_name!~"last9_scheduled_search"`)

		// Build the filter string
		var filterStr string
		if len(labelFilters) > 0 {
			filterStr = "{" + strings.Join(labelFilters, ",") + "}"
		}

		// Build PromQL query for change events
		promql := fmt.Sprintf("last9_change_events%s", filterStr)

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
			"count":                 len(changeEvents),
			"time_range": map[string]any{
				"start": time.Unix(startTimeParam, 0).Format(time.RFC3339),
				"end":   time.Unix(endTimeParam, 0).Format(time.RFC3339),
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

// fetchAvailableEventNames fetches all available event_name values from the last9_change_events metric
func fetchAvailableEventNames(ctx context.Context, client *http.Client, startTime, endTime int64, cfg models.Config) ([]string, error) {
	// Use the label values API to get all event_name values
	resp, err := utils.MakePromLabelValuesAPIQuery(ctx, client, "event_type", "last9_change_events", startTime, endTime, cfg)
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

	// Parse the response to extract event names
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
