package logs

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
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

Returns a list of log entries with full details including message content, timestamps, severity, and attributes.

- If unsure of the service or env name, call "did_you_mean" first to find the correct spelling.`

// ServiceLogsResponse represents the response structure for service logs
type ServiceLogsResponse struct {
	Service       string     `json:"service"`
	StartTime     string     `json:"start_time"`
	EndTime       string     `json:"end_time"`
	Count         int        `json:"count"`
	Logs          []LogEntry `json:"logs"`
	PartialResult bool       `json:"partial_result,omitempty"`
	Warning       string     `json:"warning,omitempty"`
	NextSteps     []string   `json:"next_steps,omitempty"`
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
	LookbackMinutes int      `json:"lookback_minutes,omitempty" jsonschema:"Number of minutes to look back from current time if start_time_iso not provided (default: 60, minimum: 1)"`
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

		normalizedIndex, err := utils.NormalizeLogIndex(args.Index)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid index: %w", err)
		}

		logjsonQuery := buildServiceLogsQuery(args.Service, args.SeverityFilters, args.BodyFilters)
		if args.Env != "" {
			logjsonQuery = addServiceLogsEnvFilter(logjsonQuery, args.Env)
		}

		// Fetch raw logs using the existing logs API approach. When index is omitted,
		// keep the query on the no-index path that matches the live dashboard/API.
		logs, err := fetchServiceLogs(ctx, client, cfg, args.Service, startTime, endTime, limit, logjsonQuery, normalizedIndex)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to fetch service logs: %w", err)
		}

		// Format response as JSON for better readability
		responseJSON, err := json.MarshalIndent(logs, "", "  ")
		if err != nil {
			return nil, nil, fmt.Errorf("failed to format response: %w", err)
		}

		// Build deep link URL with filters matching dashboard conventions
		dlBuilder := deeplink.NewBuilder(cfg.OrgSlug, cfg.ClusterID)
		dashboardQuery := cloneLogJSONQuery(logjsonQuery)

		dashboardIndex := ""
		if normalizedIndex != "" {
			resolvedIndex, err := utils.ResolveLogIndexDashboardParam(ctx, client, cfg, normalizedIndex)
			if err == nil {
				dashboardIndex = resolvedIndex
			}
		}
		dashboardURL := dlBuilder.BuildLogsLink(startTime.UnixMilli(), endTime.UnixMilli(), dashboardQuery, dashboardIndex)
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

func buildServiceLogsQuery(service string, severityFilters []string, bodyFilters []string) []map[string]interface{} {
	andConditions := []interface{}{
		map[string]interface{}{
			"$eq": []interface{}{"ServiceName", service},
		},
	}

	if severityCondition := buildServiceLogsORCondition("SeverityText", severityFilters, "$ieq"); severityCondition != nil {
		andConditions = append(andConditions, severityCondition)
	}
	if bodyCondition := buildServiceLogsORCondition("Body", bodyFilters, "$icontains"); bodyCondition != nil {
		andConditions = append(andConditions, bodyCondition)
	}

	return []map[string]interface{}{
		{
			"type": "filter",
			"query": map[string]interface{}{
				"$and": andConditions,
			},
		},
	}
}

func buildServiceLogsORCondition(field string, filters []string, operator string) map[string]interface{} {
	conditions := make([]interface{}, 0, len(filters))
	for _, filter := range filters {
		trimmed := strings.TrimSpace(filter)
		if trimmed == "" {
			continue
		}
		conditions = append(conditions, map[string]interface{}{
			operator: []interface{}{field, trimmed},
		})
	}

	switch len(conditions) {
	case 0:
		return nil
	case 1:
		condition, _ := conditions[0].(map[string]interface{})
		return condition
	default:
		return map[string]interface{}{
			"$or": conditions,
		}
	}
}

func cloneLogJSONQuery(query []map[string]interface{}) []map[string]interface{} {
	cloned := make([]map[string]interface{}, len(query))
	for i, stage := range query {
		cloned[i] = mapsClone(stage)
	}
	return cloned
}

func addServiceLogsEnvFilter(query []map[string]interface{}, env string) []map[string]interface{} {
	trimmedEnv := strings.TrimSpace(env)
	if trimmedEnv == "" || len(query) == 0 {
		return query
	}

	filterStage := mapsClone(query[0])
	queryMap, ok := filterStage["query"].(map[string]interface{})
	if !ok {
		return query
	}

	clonedQuery := mapsClone(queryMap)
	andConditions, ok := clonedQuery["$and"].([]interface{})
	if !ok {
		return query
	}

	clonedConditions := append([]interface{}(nil), andConditions...)
	clonedConditions = append(clonedConditions, map[string]interface{}{
		"$ieq": []interface{}{"attributes['deployment_environment']", trimmedEnv},
	})
	clonedQuery["$and"] = clonedConditions
	filterStage["query"] = clonedQuery
	query[0] = filterStage
	return query
}

// fetchServiceLogs retrieves raw log entries for a specific service using utils package
func fetchServiceLogs(ctx context.Context, client *http.Client, cfg models.Config, service string, startTime, endTime time.Time, limit int, logjsonQuery []map[string]interface{}, index string) (*ServiceLogsResponse, error) {
	chunks := utils.GetTimeRangeChunksBackward(startTime.UnixMilli(), endTime.UnixMilli())
	logs := make([]LogEntry, 0, limit)
	chunkingDebug := chunkingDebugEnabled()
	var partialErr error

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
			logjsonQuery,
			chunk.StartMs,
			chunk.EndMs,
			remaining,
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
			if len(logs) > 0 {
				partialErr = fmt.Errorf("chunk %d/%d failed: %w", chunkIndex+1, len(chunks), err)
				break
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
		if partialErr != nil {
			log.Printf(
				"[chunking] get_service_logs chunking partial service=%q returned_entries=%d start_ms=%d end_ms=%d err=%v",
				service,
				len(logs),
				startTime.UnixMilli(),
				endTime.UnixMilli(),
				partialErr,
			)
		} else {
			log.Printf(
				"[chunking] get_service_logs chunking complete service=%q returned_entries=%d start_ms=%d end_ms=%d",
				service,
				len(logs),
				startTime.UnixMilli(),
				endTime.UnixMilli(),
			)
		}
	}

	response := &ServiceLogsResponse{
		Service:   service,
		StartTime: startTime.Format(time.RFC3339),
		EndTime:   endTime.Format(time.RFC3339),
		Count:     len(logs),
		Logs:      logs,
	}
	if partialErr != nil {
		response.PartialResult = true
		response.Warning = fmt.Sprintf("Returning partial results: %v", partialErr)
	}
	if len(logs) == 0 {
		response.NextSteps = emptyLogsNextSteps(service)
	}

	return response, nil
}

func fetchServiceLogsChunk(ctx context.Context, client *http.Client, cfg models.Config, service string, logjsonQuery []map[string]interface{}, startTimeMs, endTimeMs int64, limit int, index string) ([]LogEntry, error) {
	apiResponse, err := executeLogJSONQuery(ctx, client, cfg, logjsonQuery, startTimeMs, endTimeMs, limit, index)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
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

	resultType, _ := data["resultType"].(string)
	rawResult, exists := data["result"]
	if !exists || rawResult == nil {
		if resultType == "streams" {
			return logs
		}
		if chunkingDebug {
			log.Printf(
				"[chunking] get_service_logs parse missing result array service=%q data=%#v",
				service,
				data,
			)
		}
		return logs
	}

	result, ok := rawResult.([]any)
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
