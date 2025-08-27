package traces

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/acrmp/mcp"
)

const (
	LookbackMinutesDefault = 60
	LimitDefault           = 10
	OrderDefault           = "Duration"
	DirectionDefault       = "backward"
)

// TraceQueryPipeline represents the pipeline structure for trace queries
type TraceQueryPipeline struct {
	Pipeline []PipelineStep `json:"pipeline"`
}

// PipelineStep represents a single step in the trace query pipeline
type PipelineStep struct {
	Query map[string]interface{} `json:"query"`
	Type  string                 `json:"type"`
}

// TraceQueryParams holds all the parameters for a trace query
type TraceQueryParams struct {
	ServiceName     string
	LookbackMinutes int
	Region          string
	Limit           int
	Order           string
	Direction       string
	SpanKinds       []string
	SpanName        string
	StatusCodes     []string
}

type TraceData struct {
	TraceID     string `json:"trace_id"`
	SpanID      string `json:"span_id"`
	SpanKind    string `json:"span_kind"`
	SpanName    string `json:"span_name"`
	ServiceName string `json:"service_name"`
	Duration    int64  `json:"duration"`
	Timestamp   int64  `json:"timestamp"`
	TraceState  string `json:"trace_state"`
	StatusCode  string `json:"status_code"`
}

type TraceQueryResponse struct {
	Data    []TraceData `json:"data"`
	Success bool        `json:"success"`
	Message string      `json:"message"`
}

// Span kind constants
const (
	SpanKindServer   = "SPAN_KIND_SERVER"
	SpanKindClient   = "SPAN_KIND_CLIENT"
	SpanKindInternal = "SPAN_KIND_INTERNAL"
	SpanKindConsumer = "SPAN_KIND_CONSUMER"
	SpanKindProducer = "SPAN_KIND_PRODUCER"
)

// Status code constants
const (
	StatusCodeUnset = "STATUS_CODE_UNSET"
	StatusCodeError = "STATUS_CODE_ERROR"
	StatusCodeOK    = "STATUS_CODE_OK"
)

// spanKindMapping maps user-friendly terms to span kind constants
var spanKindMapping = map[string]string{
	"server":   SpanKindServer,
	"client":   SpanKindClient,
	"internal": SpanKindInternal,
	"consumer": SpanKindConsumer,
	"producer": SpanKindProducer,
	// Also support the full constants directly
	"span_kind_server":   SpanKindServer,
	"span_kind_client":   SpanKindClient,
	"span_kind_internal": SpanKindInternal,
	"span_kind_consumer": SpanKindConsumer,
	"span_kind_producer": SpanKindProducer,
}

// statusCodeMapping maps user-friendly terms to status code constants
var statusCodeMapping = map[string]string{
	"unset":   StatusCodeUnset,
	"error":   StatusCodeError,
	"ok":      StatusCodeOK,
	"success": StatusCodeOK,
	// Also support the full constants directly
	"status_code_unset": StatusCodeUnset,
	"status_code_error": StatusCodeError,
	"status_code_ok":    StatusCodeOK,
}

// parseTraceQueryParams extracts and validates parameters from MCP request
func parseTraceQueryParams(params mcp.CallToolRequestParams, cfg models.Config) (*TraceQueryParams, error) {
	// Required parameter
	serviceName, ok := params.Arguments["service_name"].(string)
	if !ok {
		return nil, errors.New("service_name is required")
	}
	if serviceName == "" {
		return nil, errors.New("service_name cannot be empty")
	}

	// Parse parameters with defaults
	queryParams := &TraceQueryParams{
		ServiceName:     serviceName,
		LookbackMinutes: LookbackMinutesDefault,
		Region:          utils.GetDefaultRegion(cfg.BaseURL),
		Limit:           LimitDefault,
		Order:           OrderDefault,
		Direction:       DirectionDefault,
	}

	// Override defaults with provided values
	if l, ok := params.Arguments["lookback_minutes"].(float64); ok {
		queryParams.LookbackMinutes = int(l)
	}
	if l, ok := params.Arguments["limit"].(float64); ok {
		queryParams.Limit = int(l)
	}
	if o, ok := params.Arguments["order"].(string); ok && o != "" {
		queryParams.Order = o
	}
	if d, ok := params.Arguments["direction"].(string); ok && d != "" {
		queryParams.Direction = d
	}
	if sn, ok := params.Arguments["span_name"].(string); ok && sn != "" {
		queryParams.SpanName = sn
	}

	// Parse array parameters with mapping
	queryParams.SpanKinds = mapSpanKinds(utils.ParseStringArray(params.Arguments["span_kind"]))
	queryParams.StatusCodes = mapStatusCodes(utils.ParseStringArray(params.Arguments["status_code"]))

	return queryParams, nil
}

// mapSpanKinds converts user-friendly span kind terms to constants
func mapSpanKinds(userValues []string) []string {
	var mapped []string
	for _, value := range userValues {
		if constant, exists := spanKindMapping[strings.ToLower(value)]; exists {
			mapped = append(mapped, constant)
		} else {
			// If no mapping found, use the original value (might already be a constant)
			mapped = append(mapped, value)
		}
	}
	return mapped
}

// mapStatusCodes converts user-friendly status code terms to constants
func mapStatusCodes(userValues []string) []string {
	var mapped []string
	for _, value := range userValues {
		if constant, exists := statusCodeMapping[strings.ToLower(value)]; exists {
			mapped = append(mapped, constant)
		} else {
			// If no mapping found, use the original value (might already be a constant)
			mapped = append(mapped, value)
		}
	}
	return mapped
}

// buildTraceFilters creates the filter conditions for the trace query
func buildTraceFilters(params *TraceQueryParams) []map[string]interface{} {
	filters := []map[string]interface{}{
		{"$eq": []interface{}{"ServiceName", params.ServiceName}},
	}

	// Add span kind filters
	if len(params.SpanKinds) > 0 {
		filters = append(filters, utils.BuildOrFilter("SpanKind", params.SpanKinds))
	}

	// Add span name filter
	if params.SpanName != "" {
		filters = append(filters, map[string]interface{}{
			"$eq": []interface{}{"SpanName", params.SpanName},
		})
	}

	// Add status code filters
	if len(params.StatusCodes) > 0 {
		filters = append(filters, utils.BuildOrFilter("StatusCode", params.StatusCodes))
	}

	return filters
}

// buildRequestURL constructs the API endpoint URL with query parameters
func buildRequestURL(cfg models.Config, params *TraceQueryParams, startTime, endTime int64) (*url.URL, error) {
	u, err := url.Parse(cfg.APIBaseURL + "/cat/api/traces/v2/query_range/json")
	if err != nil {
		return nil, fmt.Errorf("failed to parse URL: %w", err)
	}

	q := u.Query()
	q.Set("region", params.Region)
	q.Set("start", strconv.FormatInt(startTime, 10))
	q.Set("end", strconv.FormatInt(endTime, 10))
	q.Set("limit", strconv.Itoa(params.Limit))
	q.Set("order", params.Order)
	q.Set("direction", params.Direction)
	u.RawQuery = q.Encode()

	return u, nil
}

// createTraceRequest builds the HTTP request with proper headers and payload
func createTraceRequest(requestURL *url.URL, filters []map[string]interface{}, cfg models.Config) (*http.Request, error) {
	// Create pipeline payload
	pipeline := TraceQueryPipeline{
		Pipeline: []PipelineStep{{
			Query: map[string]interface{}{"$and": filters},
			Type:  "filter",
		}},
	}

	payloadBytes, err := json.Marshal(pipeline)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequest("POST", requestURL.String(), bytes.NewBuffer(payloadBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	// Handle Bearer token authentication
	accessToken := cfg.AccessToken
	if !strings.HasPrefix(accessToken, "Bearer ") {
		accessToken = "Bearer " + accessToken
	}
	req.Header.Set("Authorization", accessToken)
	req.Header.Set("X-LAST9-API-TOKEN", accessToken)

	return req, nil
}

// transformToTraceQueryResponse converts raw API response to structured TraceQueryResponse
func transformToTraceQueryResponse(rawResult map[string]interface{}) TraceQueryResponse {
	response := TraceQueryResponse{
		Success: true,
		Message: "Traces retrieved successfully",
		Data:    []TraceData{},
	}

	// Check if the response has the expected structure
	data, ok := rawResult["data"].(map[string]interface{})
	if !ok {
		response.Success = false
		response.Message = "Invalid response structure: missing data field"
		return response
	}

	result, ok := data["result"].([]interface{})
	if !ok {
		response.Success = false
		response.Message = "Invalid response structure: missing result array"
		return response
	}

	// Transform each trace item
	for _, item := range result {
		traceItem, ok := item.(map[string]interface{})
		if !ok {
			continue
		}

		traceData := TraceData{
			TraceID:     utils.GetStringValue(traceItem, "TraceId"),
			SpanID:      utils.GetStringValue(traceItem, "SpanId"),
			SpanKind:    utils.GetStringValue(traceItem, "SpanKind"),
			SpanName:    utils.GetStringValue(traceItem, "SpanName"),
			ServiceName: utils.GetStringValue(traceItem, "ServiceName"),
			Duration:    utils.GetInt64Value(traceItem, "Duration"),
			Timestamp:   utils.ParseTimestamp(utils.GetStringValue(traceItem, "Timestamp")),
			TraceState:  utils.GetStringValue(traceItem, "TraceState"),
			StatusCode:  utils.GetStringValue(traceItem, "StatusCode"),
		}

		response.Data = append(response.Data, traceData)
	}

	return response
}

// NewGetServiceTraceHandler creates a handler for querying service traces
func NewGetServiceTraceHandler(client *http.Client, cfg models.Config) func(mcp.CallToolRequestParams) (mcp.CallToolResult, error) {
	return func(params mcp.CallToolRequestParams) (mcp.CallToolResult, error) {
		// Parse and validate parameters
		queryParams, err := parseTraceQueryParams(params, cfg)
		if err != nil {
			return mcp.CallToolResult{}, err
		}

		// Get time range
		startTime, endTime, err := utils.GetTimeRange(params.Arguments, queryParams.LookbackMinutes)
		if err != nil {
			return mcp.CallToolResult{}, err
		}

		// Build request URL
		requestURL, err := buildRequestURL(cfg, queryParams, startTime.Unix(), endTime.Unix())
		if err != nil {
			return mcp.CallToolResult{}, err
		}

		// Build filters
		filters := buildTraceFilters(queryParams)

		// Create HTTP request
		req, err := createTraceRequest(requestURL, filters, cfg)
		if err != nil {
			return mcp.CallToolResult{}, err
		}

		// Execute request
		resp, err := client.Do(req)
		if err != nil {
			return mcp.CallToolResult{}, fmt.Errorf("request failed: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return mcp.CallToolResult{}, fmt.Errorf("API request failed with status %d: %s",
				resp.StatusCode, string(body))
		}

		var rawResult map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&rawResult); err != nil {
			return mcp.CallToolResult{}, fmt.Errorf("failed to decode response: %w", err)
		}

		// Transform raw response to structured TraceQueryResponse
		traceResponse := transformToTraceQueryResponse(rawResult)

		jsonData, err := json.Marshal(traceResponse)
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
