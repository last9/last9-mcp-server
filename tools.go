package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/acrmp/mcp"
	"golang.org/x/time/rate"
)

// createTools creates the MCP tool definitions with appropriate rate limits
func createTools(cfg config) ([]mcp.ToolDefinition, error) {
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	return []mcp.ToolDefinition{
		{
			Metadata: mcp.Tool{
				Name:        "get_exceptions",
				Description: ptr(getExceptionsDescription),
				InputSchema: mcp.ToolInputSchema{
					Type: "object",
					Properties: mcp.ToolInputSchemaProperties{
						"limit": map[string]any{
							"type":        "integer",
							"description": "Maximum number of exceptions to return",
							"default":     20,
						},
						"start_time_iso": map[string]any{
							"type":        "string",
							"description": "Start time in ISO format (YYYY-MM-DD HH:MM:SS)",
						},
						"end_time_iso": map[string]any{
							"type":        "string",
							"description": "End time in ISO format (YYYY-MM-DD HH:MM:SS)",
						},
						"span_name": map[string]any{
							"type":        "string",
							"description": "Name of the span to filter by",
						},
					},
				},
			},
			Execute:   newGetExceptionsHandler(client, cfg),
			RateLimit: rate.NewLimiter(rate.Limit(cfg.requestRateLimit), cfg.requestRateBurst),
		},
		{
			Metadata: mcp.Tool{
				Name:        "get_service_graph",
				Description: ptr(getServiceGraphDescription),
				InputSchema: mcp.ToolInputSchema{
					Type: "object",
					Properties: mcp.ToolInputSchemaProperties{
						"span_name": map[string]any{
							"type":        "string",
							"description": "Name of the span to get dependencies for",
						},
						"lookback_minutes": map[string]any{
							"type":        "integer",
							"description": "Number of minutes to look back",
							"default":     60,
						},
						"start_time_iso": map[string]any{
							"type":        "string",
							"description": "Start time in ISO format (YYYY-MM-DD HH:MM:SS)",
						},
					},
				},
			},
			Execute:   newGetServiceGraphHandler(client, cfg),
			RateLimit: rate.NewLimiter(rate.Limit(cfg.requestRateLimit), cfg.requestRateBurst),
		},
		{
			Metadata: mcp.Tool{
				Name:        "get_logs",
				Description: ptr(getLogsDescription),
				InputSchema: mcp.ToolInputSchema{
					Type: "object",
					Properties: mcp.ToolInputSchemaProperties{
						"service": map[string]any{
							"type":        "string",
							"description": "Name of the service to get logs for",
						},
						"severity": map[string]any{
							"type":        "string",
							"description": "Severity of the logs to get",
						},
						"start_time_iso": map[string]any{
							"type":        "string",
							"description": "Start time in ISO format (YYYY-MM-DD HH:MM:SS)",
						},
						"end_time_iso": map[string]any{
							"type":        "string",
							"description": "End time in ISO format (YYYY-MM-DD HH:MM:SS)",
						},
					},
				},
			},
			Execute:   newGetLogsHandler(client, cfg),
			RateLimit: rate.NewLimiter(rate.Limit(cfg.requestRateLimit), cfg.requestRateBurst),
		},
	}, nil
}

// ptr returns a pointer to the provided string
func ptr(s string) *string {
	return &s
}

const getExceptionsDescription = `Get server side exceptions over the given time range. 
Includes the exception type, message, stack trace, service name, trace ID and span attributes.`

const getServiceGraphDescription = `Gets the upstream and downstream services for a given span name, 
along with the throughput for each service.`

const getLogsDescription = `Get logs filtered by optional service name and/or severity level within a specified time range. 
Omitting service returns logs from all services.`

// newGetExceptionsHandler creates a handler for getting exceptions
func newGetExceptionsHandler(client *http.Client, cfg config) func(mcp.CallToolRequestParams) (mcp.CallToolResult, error) {
	return func(params mcp.CallToolRequestParams) (mcp.CallToolResult, error) {
		limit := 20
		if l, ok := params.Arguments["limit"].(float64); ok {
			limit = int(l)
		}

		// Calculate timestamps
		endTime := time.Now()
		startTime := endTime.Add(-1 * time.Hour)

		if startTimeStr, ok := params.Arguments["start_time_iso"].(string); ok && startTimeStr != "" {
			t, err := time.Parse("2006-01-02 15:04:05", startTimeStr)
			if err != nil {
				return mcp.CallToolResult{}, fmt.Errorf("invalid start_time_iso format: %w", err)
			}
			startTime = t
		}

		if endTimeStr, ok := params.Arguments["end_time_iso"].(string); ok && endTimeStr != "" {
			t, err := time.Parse("2006-01-02 15:04:05", endTimeStr)
			if err != nil {
				return mcp.CallToolResult{}, fmt.Errorf("invalid end_time_iso format: %w", err)
			}
			endTime = t
		}

		// Build request URL with query parameters
		u, err := url.Parse(cfg.baseURL + "/telemetry/api/v1/exceptions")
		if err != nil {
			return mcp.CallToolResult{}, fmt.Errorf("failed to parse URL: %w", err)
		}

		q := u.Query()
		q.Set("start", strconv.FormatInt(startTime.Unix(), 10))
		q.Set("end", strconv.FormatInt(endTime.Unix(), 10))
		q.Set("limit", strconv.Itoa(limit))

		if spanName, ok := params.Arguments["span_name"].(string); ok && spanName != "" {
			q.Set("span_name", spanName)
		}

		u.RawQuery = q.Encode()

		// Create request
		req, err := http.NewRequest("GET", u.String(), nil)
		if err != nil {
			return mcp.CallToolResult{}, fmt.Errorf("failed to create request: %w", err)
		}

		req.Header.Set("Authorization", "Basic "+cfg.authToken)

		// Execute request
		resp, err := client.Do(req)
		if err != nil {
			return mcp.CallToolResult{}, fmt.Errorf("request failed: %w", err)
		}
		defer resp.Body.Close()

		var result interface{}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return mcp.CallToolResult{}, fmt.Errorf("failed to decode response: %w", err)
		}

		jsonData, err := json.Marshal(result)
		if err != nil {
			return mcp.CallToolResult{}, fmt.Errorf("failed to marshal response: %w", err)
		}

		return mcp.CallToolResult{
			Content: []any{
				mcp.TextContent{
					Text: string(jsonData),
					Type: "text",
				},
			},
		}, nil
	}
}

// newGetServiceGraphHandler creates a handler for getting service dependencies
func newGetServiceGraphHandler(client *http.Client, cfg config) func(mcp.CallToolRequestParams) (mcp.CallToolResult, error) {
	return func(params mcp.CallToolRequestParams) (mcp.CallToolResult, error) {
		spanName, ok := params.Arguments["span_name"].(string)
		if !ok || spanName == "" {
			return mcp.CallToolResult{}, errors.New("span_name is required")
		}

		lookbackMinutes := 60
		if l, ok := params.Arguments["lookback_minutes"].(float64); ok {
			lookbackMinutes = int(l)
		}

		startTime := time.Now()
		if startTimeStr, ok := params.Arguments["start_time_iso"].(string); ok && startTimeStr != "" {
			t, err := time.Parse("2006-01-02 15:04:05", startTimeStr)
			if err != nil {
				return mcp.CallToolResult{}, fmt.Errorf("invalid start_time_iso format: %w", err)
			}
			startTime = t
		}

		// Build request URL
		u, err := url.Parse(cfg.baseURL + "/telemetry/api/v1/service_graph")
		if err != nil {
			return mcp.CallToolResult{}, fmt.Errorf("failed to parse URL: %w", err)
		}

		q := u.Query()
		q.Set("start", strconv.FormatInt(startTime.Unix(), 10))
		q.Set("span_name", spanName)
		q.Set("lookback_minutes", strconv.Itoa(lookbackMinutes))
		u.RawQuery = q.Encode()

		// Create request
		req, err := http.NewRequest("GET", u.String(), nil)
		if err != nil {
			return mcp.CallToolResult{}, fmt.Errorf("failed to create request: %w", err)
		}

		req.Header.Set("Authorization", "Basic "+cfg.authToken)

		// Execute request
		resp, err := client.Do(req)
		if err != nil {
			return mcp.CallToolResult{}, fmt.Errorf("request failed: %w", err)
		}
		defer resp.Body.Close()

		var result interface{}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return mcp.CallToolResult{}, fmt.Errorf("failed to decode response: %w", err)
		}

		jsonData, err := json.Marshal(result)
		if err != nil {
			return mcp.CallToolResult{}, fmt.Errorf("failed to marshal response: %w", err)
		}

		return mcp.CallToolResult{
			Content: []any{
				mcp.TextContent{
					Text: string(jsonData),
					Type: "text",
				},
			},
		}, nil
	}
}

// newGetLogsHandler creates a handler for getting logs
func newGetLogsHandler(client *http.Client, cfg config) func(mcp.CallToolRequestParams) (mcp.CallToolResult, error) {
	return func(params mcp.CallToolRequestParams) (mcp.CallToolResult, error) {
		limit := 20
		if l, ok := params.Arguments["limit"].(float64); ok {
			limit = int(l)
		}

		// Calculate timestamps
		endTime := time.Now()
		startTime := endTime.Add(-1 * time.Hour)

		if startTimeStr, ok := params.Arguments["start_time_iso"].(string); ok && startTimeStr != "" {
			t, err := time.Parse("2006-01-02 15:04:05", startTimeStr)
			if err != nil {
				return mcp.CallToolResult{}, fmt.Errorf("invalid start_time_iso format: %w", err)
			}
			startTime = t
		}

		if endTimeStr, ok := params.Arguments["end_time_iso"].(string); ok && endTimeStr != "" {
			t, err := time.Parse("2006-01-02 15:04:05", endTimeStr)
			if err != nil {
				return mcp.CallToolResult{}, fmt.Errorf("invalid end_time_iso format: %w", err)
			}
			endTime = t
		}

		// Build request URL with query parameters
		u, err := url.Parse(cfg.baseURL + "/telemetry/api/v1/logs")
		if err != nil {
			return mcp.CallToolResult{}, fmt.Errorf("failed to parse URL: %w", err)
		}

		q := u.Query()
		q.Set("start", strconv.FormatInt(startTime.Unix(), 10))
		q.Set("end", strconv.FormatInt(endTime.Unix(), 10))
		q.Set("limit", strconv.Itoa(limit))

		if service, ok := params.Arguments["service"].(string); ok && service != "" {
			q.Set("service", service)
		}

		if severity, ok := params.Arguments["severity"].(string); ok && severity != "" {
			q.Set("severity", severity)
		}

		u.RawQuery = q.Encode()

		// Create request
		req, err := http.NewRequest("GET", u.String(), nil)
		if err != nil {
			return mcp.CallToolResult{}, fmt.Errorf("failed to create request: %w", err)
		}

		req.Header.Set("Authorization", "Basic "+cfg.authToken)

		// Execute request
		resp, err := client.Do(req)
		if err != nil {
			return mcp.CallToolResult{}, fmt.Errorf("request failed: %w", err)
		}
		defer resp.Body.Close()

		var result interface{}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return mcp.CallToolResult{}, fmt.Errorf("failed to decode response: %w", err)
		}

		jsonData, err := json.Marshal(result)
		if err != nil {
			return mcp.CallToolResult{}, fmt.Errorf("failed to marshal response: %w", err)
		}

		return mcp.CallToolResult{
			Content: []any{
				mcp.TextContent{
					Text: string(jsonData),
					Type: "text",
				},
			},
		}, nil
	}
}
