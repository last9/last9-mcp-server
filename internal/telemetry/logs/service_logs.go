package logs

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/acrmp/mcp"
)

// GetServiceLogsDescription provides the description for the service logs tool
const GetServiceLogsDescription = `Get raw log entries for a specific service over a time range.

This tool retrieves actual log entries for a specified service, including log messages, timestamps, severity levels, and other metadata.
It's useful for debugging issues, monitoring service behavior, and analyzing specific log patterns.

Filtering behavior:
- severity_filters: Array of severity patterns (e.g., ["error", "warn"]) - uses OR logic (matches any pattern)
- body_filters: Array of message content patterns (e.g., ["timeout", "failed"]) - uses OR logic (matches any pattern)
- Multiple filter types are combined with AND logic (service AND severity AND body)

Examples:
1. service_name="api" + severity_filters=["error"] + body_filters=["timeout"]
   → finds error logs containing "timeout" for the "api" service
2. service_name="web" + body_filters=["timeout", "failed", "error 500"]
   → finds logs containing "timeout" OR "failed" OR "error 500" for the "web" service
3. service_name="db" + severity_filters=["error", "critical"] + body_filters=["connection", "deadlock"]
   → finds error/critical logs containing "connection" OR "deadlock" for the "db" service

Note: This tool returns raw log entries.

Parameters:
- service_name: (Required) Name of the service to get logs for
- lookback_minutes: (Optional) Number of minutes to look back from now. Default: 60 minutes
- limit: (Optional) Maximum number of log entries to return. Default: 20
- env: (Optional) Environment to filter by. Use "get_service_environments" tool to get available environments.
- severity_filters: (Optional) Array of severity patterns to filter logs
- body_filters: (Optional) Array of message content patterns to filter logs

Returns a list of log entries with full details including message content, timestamps, severity, and attributes.`

// ServiceLogsResponse represents the response structure for service logs
type ServiceLogsResponse struct {
	Service   string     `json:"service"`
	StartTime string     `json:"start_time"`
	EndTime   string     `json:"end_time"`
	Count     int        `json:"count"`
	Logs      []LogEntry `json:"logs"`
}

// LogEntry represents a single log entry
type LogEntry struct {
	Timestamp   string `json:"timestamp"`
	Message     string `json:"message"`
	Severity    string `json:"severity"`
	ServiceName string `json:"service_name"`
}

// NewGetServiceLogsHandler creates a new handler for the get_service_logs tool
func NewGetServiceLogsHandler(client *http.Client, cfg models.Config) func(mcp.CallToolRequestParams) (mcp.CallToolResult, error) {
	return func(params mcp.CallToolRequestParams) (mcp.CallToolResult, error) {
		// Validate required parameters
		service, ok := params.Arguments["service_name"].(string)
		if !ok || service == "" {
			return mcp.CallToolResult{}, fmt.Errorf("service_name parameter is required")
		}

		// Get limit parameter
		limit := 20
		if l, ok := params.Arguments["limit"].(float64); ok {
			limit = int(l)
		}

		// Get severity filters parameter (array allows multiple severity levels to be matched with OR logic)
		var severityFilters []string
		if filters, ok := params.Arguments["severity_filters"].([]interface{}); ok {
			for _, filter := range filters {
				if filterStr, ok := filter.(string); ok {
					severityFilters = append(severityFilters, filterStr)
				}
			}
		}

		// Get body filters parameter (array allows multiple message patterns to be matched with OR logic)
		var bodyFilters []string
		if filters, ok := params.Arguments["body_filters"].([]interface{}); ok {
			for _, filter := range filters {
				if filterStr, ok := filter.(string); ok {
					bodyFilters = append(bodyFilters, filterStr)
				}
			}
		}

		// Get time range using existing utility
		startTime, endTime, err := utils.GetTimeRange(params.Arguments, 60) // Default 60 minutes lookback
		if err != nil {
			return mcp.CallToolResult{}, fmt.Errorf("invalid time range: %w", err)
		}

		// Fetch physical index before making logs queries
		// Extract environment parameter if available
		env := ""
		if envParam, ok := params.Arguments["env"].(string); ok {
			env = envParam
		}

		physicalIndex, err := utils.FetchPhysicalIndex(client, cfg, service, env)
		if err != nil {
			// Log the error but continue without index to maintain backward compatibility
			fmt.Printf("Warning: failed to fetch physical index for service %s: %v\n", service, err)
			physicalIndex = ""
		}

		// Fetch raw logs using the existing logs API approach with physical index
		logs, err := fetchServiceLogs(client, cfg, service, startTime, endTime, limit, severityFilters, bodyFilters, physicalIndex)
		if err != nil {
			return mcp.CallToolResult{}, fmt.Errorf("failed to fetch service logs: %w", err)
		}

		// Format response as JSON for better readability
		responseJSON, err := json.MarshalIndent(logs, "", "  ")
		if err != nil {
			return mcp.CallToolResult{}, fmt.Errorf("failed to format response: %w", err)
		}

		return mcp.CallToolResult{
			Content: []any{
				mcp.TextContent{
					Text: string(responseJSON),
					Type: "text",
				},
			},
		}, nil
	}
}

// fetchServiceLogs retrieves raw log entries for a specific service using utils package
func fetchServiceLogs(client *http.Client, cfg models.Config, service string, startTime, endTime time.Time, limit int, severityFilters []string, bodyFilters []string, physicalIndex string) (*ServiceLogsResponse, error) {
	// Convert time.Time to Unix milliseconds for the utils function
	startTimeMs := startTime.UnixMilli()
	endTimeMs := endTime.UnixMilli()

	// Create API request struct with physical index
	apiRequest := utils.CreateServiceLogsAPIRequest(service, startTimeMs, endTimeMs, severityFilters, bodyFilters, physicalIndex)

	// Use the existing utils function to make the API call
	resp, err := utils.MakeServiceLogsAPI(client, apiRequest, &cfg)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	// Parse the raw response - the utils function returns aggregated data, not raw logs
	// We need to extract the actual log entries from the response
	var apiResponse map[string]any
	if err := json.Unmarshal(bodyBytes, &apiResponse); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Extract logs from the API response structure
	logs := make([]LogEntry, 0)

	// Navigate through the response structure: data -> result (array of streams)
	if data, ok := apiResponse["data"].(map[string]any); ok {
		if result, ok := data["result"].([]any); ok {
			for i, item := range result {
				if i >= limit {
					break
				}

				if streamData, ok := item.(map[string]any); ok {
					// Extract stream metadata
					var streamMetadata map[string]any
					var values [][]any

					if stream, exists := streamData["stream"].(map[string]any); exists {
						streamMetadata = stream
					}

					if vals, exists := streamData["values"].([]any); exists {
						for _, val := range vals {
							if valArray, ok := val.([]any); ok {
								values = append(values, valArray)
							}
						}
					}

					// Create log entries for each value in the stream
					for _, value := range values {
						if len(value) >= 2 {
							entry := LogEntry{
								ServiceName: service,
								Timestamp:   utils.ConvertTimestamp(value[0]),
								Message:     fmt.Sprintf("%v", value[1]),
							}

							// Extract severity from stream metadata
							if severity, exists := streamMetadata["severity"]; exists {
								entry.Severity = fmt.Sprintf("%v", severity)
							}

							logs = append(logs, entry)
						}
					}
				}
			}
		}
	}

	return &ServiceLogsResponse{
		Service:   service,
		StartTime: startTime.Format(time.RFC3339),
		EndTime:   endTime.Format(time.RFC3339),
		Count:     len(logs),
		Logs:      logs,
	}, nil
}
