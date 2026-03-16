package logs

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"last9-mcp/internal/deeplink"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
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
1. service="api" + severity_filters=["error"] + body_filters=["timeout"]
   → finds error logs containing "timeout" for the "api" service
2. service="web" + body_filters=["timeout", "failed", "error 500"]
   → finds logs containing "timeout" OR "failed" OR "error 500" for the "web" service
3. service="db" + severity_filters=["error", "critical"] + body_filters=["connection", "deadlock"]
   → finds error/critical logs containing "connection" OR "deadlock" for the "db" service

Note: This tool returns raw log entries.

Parameters:
- service: (Required) Name of the service to get logs for
- lookback_minutes: (Optional) Number of minutes to look back from now. Default: 60 minutes
- limit: (Optional) Maximum number of log entries to return. Default: 20
- env: (Optional) Environment to filter by. Use "get_service_environments" tool to get available environments.
- severity_filters: (Optional) Array of severity patterns to filter logs
- body_filters: (Optional) Array of message content patterns to filter logs
- index: (Optional) Explicit log index to query. Accepted values are physical_index:<name> and rehydration_index:<block_name>. Omit it when the user did not specify an index.
- If the user says "rehydration index X", use rehydration_index:X.
- If the user says "physical index X" or just "index X", use physical_index:X.

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

// GetServiceLogsArgs represents the input arguments for the get_service_logs tool
type GetServiceLogsArgs struct {
	Service         string   `json:"service" jsonschema:"Service name to retrieve logs for (e.g. api)"`
	StartTimeISO    string   `json:"start_time_iso,omitempty" jsonschema:"Start time in RFC3339/ISO8601 format (e.g. 2023-10-01T10:00:00Z). If not provided lookback_minutes is used"`
	EndTimeISO      string   `json:"end_time_iso,omitempty" jsonschema:"End time in RFC3339/ISO8601 format (e.g. 2023-10-01T11:00:00Z). If not provided current time is used"`
	LookbackMinutes int      `json:"lookback_minutes,omitempty" jsonschema:"Number of minutes to look back from current time if start_time_iso not provided (default: 60, range: 1-10080)"`
	Limit           int      `json:"limit,omitempty" jsonschema:"Maximum number of log entries to return (optional, default: 20)"`
	SeverityFilters []string `json:"severity_filters,omitempty" jsonschema:"Array of severity patterns to match (uses OR logic) (e.g. [error warn])"`
	BodyFilters     []string `json:"body_filters,omitempty" jsonschema:"Array of message content patterns to match (uses OR logic) (e.g. [timeout failed])"`
	Env             string   `json:"env,omitempty" jsonschema:"Environment to filter by. Empty string if environment is unknown (e.g. production)"`
	Index           string   `json:"index,omitempty" jsonschema:"Optional log index in the form physical_index:<name> or rehydration_index:<block_name>. Omit this when the user did not specify an index."`
}

// NewGetServiceLogsHandler creates a new handler for the get_service_logs tool
func NewGetServiceLogsHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, GetServiceLogsArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args GetServiceLogsArgs) (*mcp.CallToolResult, any, error) {
		// Validate required parameters
		if args.Service == "" {
			return nil, nil, fmt.Errorf("service parameter is required")
		}

		// Set default values
		limit := args.Limit
		if limit == 0 {
			limit = 20
		}

		lookbackMinutes := args.LookbackMinutes
		if lookbackMinutes == 0 {
			lookbackMinutes = 60
		}

		// Convert args to map for GetTimeRange utility
		params := make(map[string]interface{})
		if args.StartTimeISO != "" {
			params["start_time_iso"] = args.StartTimeISO
		}
		if args.EndTimeISO != "" {
			params["end_time_iso"] = args.EndTimeISO
		}

		// Get time range using existing utility
		startTime, endTime, err := utils.GetTimeRange(params, lookbackMinutes)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid time range: %w", err)
		}

		env := args.Env
		normalizedIndex, err := utils.NormalizeLogIndex(args.Index)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid index: %w", err)
		}
		if normalizedIndex == "" {
			normalizedIndex, err = utils.FetchPhysicalIndex(ctx, client, cfg, args.Service, env)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to fetch physical index: %w", err)
			}
		}

		// Fetch raw logs using the existing logs API approach with the requested or inferred index.
		logs, err := fetchServiceLogs(ctx, client, cfg, args.Service, startTime, endTime, limit, args.SeverityFilters, args.BodyFilters, normalizedIndex)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to fetch service logs: %w", err)
		}

		// Format response as JSON for better readability
		responseJSON, err := json.MarshalIndent(logs, "", "  ")
		if err != nil {
			return nil, nil, fmt.Errorf("failed to format response: %w", err)
		}

		// Build deep link URL with filters matching dashboard conventions
		// Dashboard expects a single filter stage with $and containing all conditions
		dlBuilder := deeplink.NewBuilder(cfg.OrgSlug, cfg.ClusterID)
		andConditions := []interface{}{
			map[string]interface{}{
				"$eq": []interface{}{"ServiceName", args.Service},
			},
		}

		// Add env filter if provided (uses attributes['deployment_environment'] format)
		if args.Env != "" {
			andConditions = append(andConditions, map[string]interface{}{
				"$ieq": []interface{}{"attributes['deployment_environment']", args.Env},
			})
		}

		// Add severity filters if provided (uses SeverityText with case-insensitive regex)
		if len(args.SeverityFilters) > 0 {
			orConditions := make([]interface{}, 0, len(args.SeverityFilters))
			for _, severity := range args.SeverityFilters {
				if severity != "" {
					orConditions = append(orConditions, map[string]interface{}{
						"$iregex": []interface{}{"SeverityText", severity},
					})
				}
			}
			if len(orConditions) > 0 {
				andConditions = append(andConditions, map[string]interface{}{
					"$or": orConditions,
				})
			}
		}

		// Add body filters if provided (uses Body with case-insensitive contains)
		if len(args.BodyFilters) > 0 {
			orConditions := make([]interface{}, 0, len(args.BodyFilters))
			for _, bodyPattern := range args.BodyFilters {
				if bodyPattern != "" {
					orConditions = append(orConditions, map[string]interface{}{
						"$icontains": []interface{}{"Body", bodyPattern},
					})
				}
			}
			if len(orConditions) > 0 {
				andConditions = append(andConditions, map[string]interface{}{
					"$or": orConditions,
				})
			}
		}

		pipeline := []map[string]interface{}{
			{
				"type": "filter",
				"query": map[string]interface{}{
					"$and": andConditions,
				},
			},
		}
		dashboardIndex := ""
		if normalizedIndex != "" {
			resolvedIndex, err := utils.ResolveLogIndexDashboardParam(ctx, client, cfg, normalizedIndex)
			if err == nil {
				dashboardIndex = resolvedIndex
			}
		}
		dashboardURL := dlBuilder.BuildLogsLink(startTime.UnixMilli(), endTime.UnixMilli(), pipeline, dashboardIndex)
		var meta mcp.Meta
		if normalizedIndex == "" || dashboardIndex != "" {
			meta = deeplink.ToMeta(dashboardURL)
		}

		return &mcp.CallToolResult{
			Meta: meta,
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: string(responseJSON),
				},
			},
		}, nil, nil
	}
}

// fetchServiceLogs retrieves raw log entries for a specific service using utils package
func fetchServiceLogs(ctx context.Context, client *http.Client, cfg models.Config, service string, startTime, endTime time.Time, limit int, severityFilters []string, bodyFilters []string, index string) (*ServiceLogsResponse, error) {
	chunks := utils.GetTimeRangeChunksBackward(startTime.UnixMilli(), endTime.UnixMilli())
	logs := make([]LogEntry, 0, limit)
	chunkingDebug := chunkingDebugEnabled()

	if chunkingDebug {
		log.Printf(
			"[chunking] get_service_logs chunking enabled service=%q chunks=%d start_ms=%d end_ms=%d limit=%d index=%q",
			service,
			len(chunks),
			startTime.UnixMilli(),
			endTime.UnixMilli(),
			limit,
			index,
		)
	}

	for chunkIndex, chunk := range chunks {
		remaining := limit - len(logs)
		if remaining <= 0 {
			break
		}

		if chunkingDebug {
			log.Printf(
				"[chunking] get_service_logs chunk request service=%q chunk=%d/%d start_ms=%d end_ms=%d remaining_limit=%d",
				service,
				chunkIndex+1,
				len(chunks),
				chunk.StartMs,
				chunk.EndMs,
				remaining,
			)
		}

		chunkLogs, err := fetchServiceLogsChunk(
			ctx,
			client,
			cfg,
			service,
			chunk.StartMs,
			chunk.EndMs,
			remaining,
			severityFilters,
			bodyFilters,
			index,
		)
		if err != nil {
			if chunkingDebug {
				log.Printf(
					"[chunking] get_service_logs chunk error service=%q chunk=%d/%d start_ms=%d end_ms=%d err=%v",
					service,
					chunkIndex+1,
					len(chunks),
					chunk.StartMs,
					chunk.EndMs,
					err,
				)
			}
			return nil, err
		}

		if chunkingDebug {
			log.Printf(
				"[chunking] get_service_logs chunk response service=%q chunk=%d/%d returned_entries=%d",
				service,
				chunkIndex+1,
				len(chunks),
				len(chunkLogs),
			)
		}

		if len(chunkLogs) > remaining {
			if chunkingDebug {
				log.Printf(
					"[chunking] get_service_logs chunk trim service=%q chunk=%d/%d kept_entries=%d dropped_entries=%d",
					service,
					chunkIndex+1,
					len(chunks),
					remaining,
					len(chunkLogs)-remaining,
				)
			}
			chunkLogs = chunkLogs[:remaining]
		}
		logs = append(logs, chunkLogs...)

		if chunkingDebug {
			log.Printf(
				"[chunking] get_service_logs chunk merged service=%q chunk=%d/%d total_entries=%d remaining_limit=%d",
				service,
				chunkIndex+1,
				len(chunks),
				len(logs),
				limit-len(logs),
			)
		}
	}

	if chunkingDebug {
		log.Printf(
			"[chunking] get_service_logs chunking complete service=%q returned_entries=%d start_ms=%d end_ms=%d",
			service,
			len(logs),
			startTime.UnixMilli(),
			endTime.UnixMilli(),
		)
	}

	return &ServiceLogsResponse{
		Service:   service,
		StartTime: startTime.Format(time.RFC3339),
		EndTime:   endTime.Format(time.RFC3339),
		Count:     len(logs),
		Logs:      logs,
	}, nil
}

func fetchServiceLogsChunk(ctx context.Context, client *http.Client, cfg models.Config, service string, startTimeMs, endTimeMs int64, limit int, severityFilters []string, bodyFilters []string, index string) ([]LogEntry, error) {
	apiRequest := utils.CreateServiceLogsAPIRequest(service, startTimeMs, endTimeMs, limit, severityFilters, bodyFilters, index)

	resp, err := utils.MakeServiceLogsAPI(ctx, client, apiRequest, &cfg)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var apiResponse map[string]any
	if err := json.Unmarshal(bodyBytes, &apiResponse); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return parseServiceLogEntries(apiResponse, service), nil
}

func parseServiceLogEntries(apiResponse map[string]any, service string) []LogEntry {
	logs := make([]LogEntry, 0)
	chunkingDebug := chunkingDebugEnabled()

	data, ok := apiResponse["data"].(map[string]any)
	if !ok {
		if chunkingDebug {
			log.Printf(
				"[chunking] get_service_logs parse missing data object service=%q response=%#v",
				service,
				apiResponse,
			)
		}
		return logs
	}

	result, ok := data["result"].([]any)
	if !ok {
		if chunkingDebug {
			log.Printf(
				"[chunking] get_service_logs parse missing result array service=%q data=%#v",
				service,
				data,
			)
		}
		return logs
	}

	for _, item := range result {
		streamData, ok := item.(map[string]any)
		if !ok {
			if chunkingDebug {
				log.Printf(
					"[chunking] get_service_logs parse skipped non-stream item service=%q item=%#v",
					service,
					item,
				)
			}
			continue
		}

		var streamMetadata map[string]any
		if stream, exists := streamData["stream"].(map[string]any); exists {
			streamMetadata = stream
		} else {
			if chunkingDebug {
				log.Printf(
					"[chunking] get_service_logs parse missing stream metadata service=%q item=%#v",
					service,
					item,
				)
			}
		}

		var severity any
		hasSeverity := false
		if streamMetadata != nil {
			severity, hasSeverity = streamMetadata["severity"]
			if !hasSeverity && chunkingDebug {
				log.Printf(
					"[chunking] get_service_logs parse missing severity service=%q stream=%#v",
					service,
					streamMetadata,
				)
			}
		}

		vals, ok := streamData["values"].([]any)
		if !ok {
			if chunkingDebug {
				log.Printf(
					"[chunking] get_service_logs parse missing values array service=%q item=%#v",
					service,
					item,
				)
			}
			continue
		}

		for _, val := range vals {
			valArray, ok := val.([]any)
			if !ok || len(valArray) < 2 {
				if chunkingDebug {
					log.Printf(
						"[chunking] get_service_logs parse skipped malformed value service=%q value=%#v",
						service,
						val,
					)
				}
				continue
			}

			entry := LogEntry{
				ServiceName: service,
				Timestamp:   utils.ConvertTimestamp(valArray[0]),
				Message:     fmt.Sprintf("%v", valArray[1]),
			}
			if hasSeverity {
				entry.Severity = fmt.Sprintf("%v", severity)
			}

			logs = append(logs, entry)
		}
	}

	return logs
}
