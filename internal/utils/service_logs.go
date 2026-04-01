package utils

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"last9-mcp/internal/constants"
	"last9-mcp/internal/models"
)

// Constants for service logs API
const (
	maxServiceLogsDurationHours = 24 // Maximum allowed duration for service logs
)

// ServiceLogsParams holds parameters for service logs API call
type ServiceLogsParams struct {
	Service         string
	StartTime       int64 // Unix timestamp in milliseconds
	EndTime         int64 // Unix timestamp in milliseconds
	Limit           int
	Region          string
	SeverityFilters []string // Optional regex patterns for severity filtering
	BodyFilters     []string // Optional regex patterns for body filtering
	Index           string   // Optional log index parameter for logs queries
}

func createServiceLogsParams(request ServiceLogsAPIRequest, region string) ServiceLogsParams {
	return ServiceLogsParams{
		Service:         request.Service,
		StartTime:       request.StartTime,
		EndTime:         request.EndTime,
		Limit:           request.Limit,
		Region:          region,
		SeverityFilters: request.SeverityFilters,
		BodyFilters:     request.BodyFilters,
		Index:           request.Index,
	}
}

func (p *ServiceLogsParams) Validate() error {
	if strings.TrimSpace(p.Service) == "" {
		return errors.New("service name cannot be empty")
	}
	if p.StartTime <= 0 {
		return errors.New("start time must be positive")
	}
	if p.EndTime <= 0 {
		return errors.New("end time must be positive")
	}
	if p.EndTime <= p.StartTime {
		return errors.New("end time must be after start time")
	}
	if strings.TrimSpace(p.Region) == "" {
		return errors.New("region cannot be empty")
	}

	// Check duration limits (convert milliseconds to seconds for validation)
	durationMs := p.EndTime - p.StartTime
	durationSeconds := durationMs / 1000
	maxDurationSeconds := int64(maxServiceLogsDurationHours * 3600)
	if durationSeconds > maxDurationSeconds {
		return fmt.Errorf("duration exceeds maximum allowed: %d hours (got %d seconds)", maxServiceLogsDurationHours, durationSeconds)
	}

	return nil
}

// LogsPipelineStage represents a single stage in the logs pipeline
type LogsPipelineStage struct {
	Query    any               `json:"query,omitempty"`
	Function any               `json:"function,omitempty"`
	GroupBy  map[string]string `json:"groupby,omitempty"`
	Type     string            `json:"type"`
	Window   []any             `json:"window,omitempty"`
}

// ServiceLogsRequest represents the request body for service logs API
type ServiceLogsRequest struct {
	Pipeline []LogsPipelineStage `json:"pipeline"`
}

// ServiceLogsAPIRequest contains all parameters needed for service logs API calls
type ServiceLogsAPIRequest struct {
	Service         string
	StartTime       int64 // Unix timestamp in milliseconds
	EndTime         int64 // Unix timestamp in milliseconds
	Limit           int
	SeverityFilters []string // Optional regex patterns for severity filtering
	BodyFilters     []string // Optional regex patterns for body filtering
	Index           string   // Optional log index parameter for logs queries
}

// CreateServiceLogsAPIRequest creates a new service logs API request with default options
func CreateServiceLogsAPIRequest(service string, startTime, endTime int64, limit int, severityFilters []string, bodyFilters []string, index string) ServiceLogsAPIRequest {
	return ServiceLogsAPIRequest{
		Service:         service,
		StartTime:       startTime,
		EndTime:         endTime,
		Limit:           limit,
		SeverityFilters: severityFilters,
		BodyFilters:     bodyFilters,
		Index:           index,
	}
}

// MakeServiceLogsAPI creates a service logs API request with improved error handling and validation
func MakeServiceLogsAPI(ctx context.Context, client *http.Client, request ServiceLogsAPIRequest, cfg *models.Config) (*http.Response, error) {
	// Validate inputs
	if err := validateServiceLogsInputs(client, cfg); err != nil {
		return nil, err
	}

	// Create parameters using region from config (set from datasource API response)
	params := createServiceLogsParams(request, cfg.Region)

	if err := (&params).Validate(); err != nil {
		return nil, fmt.Errorf("invalid parameters: %w", err)
	}

	// Build URL and request body
	fullURL, err := buildServiceLogsURL(cfg.APIBaseURL, params)
	if err != nil {
		return nil, fmt.Errorf("failed to build service logs URL: %w", err)
	}

	bodyBytes, err := createServiceLogsRequestBody(request.Service, params.SeverityFilters, request.BodyFilters)
	if err != nil {
		return nil, fmt.Errorf("failed to create request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fullURL, bytes.NewBuffer(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	setServiceLogsHeaders(req, cfg.TokenManager.GetAccessToken(ctx))

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}

	// Check for HTTP error status codes
	if resp.StatusCode >= 400 {
		return resp, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, resp.Status)
	}

	return resp, nil
}

// validateServiceLogsInputs validates the basic inputs
func validateServiceLogsInputs(client *http.Client, cfg *models.Config) error {
	if client == nil {
		return errors.New("http client cannot be nil")
	}
	if cfg == nil {
		return errors.New("config cannot be nil")
	}
	if strings.TrimSpace(cfg.APIBaseURL) == "" {
		return errors.New("API base URL cannot be empty")
	}
	if strings.TrimSpace(cfg.TokenManager.GetAccessToken(context.Background())) == "" {
		return errors.New("access token cannot be empty")
	}
	return nil
}

// buildServiceLogsURL constructs the full URL with query parameters for service logs API
func buildServiceLogsURL(apiBaseURL string, params ServiceLogsParams) (string, error) {
	if strings.TrimSpace(apiBaseURL) == "" {
		return "", errors.New("API base URL cannot be empty")
	}

	logsURL := fmt.Sprintf("%s%s", apiBaseURL, constants.EndpointLogsQueryRange)

	queryParams := url.Values{}
	queryParams.Add("direction", "backward")
	queryParams.Add("start", fmt.Sprintf("%d", params.StartTime/1000)) // Convert to seconds
	queryParams.Add("end", fmt.Sprintf("%d", params.EndTime/1000))     // Convert to seconds
	queryParams.Add("region", params.Region)
	if params.Limit > 0 {
		queryParams.Add("limit", fmt.Sprintf("%d", params.Limit))
	}

	normalizedIndex, err := NormalizeLogIndex(params.Index)
	if err != nil {
		return "", err
	}
	if normalizedIndex != "" {
		queryParams.Add("index", normalizedIndex)
		if strings.HasPrefix(normalizedIndex, logIndexPhysicalPrefix) {
			queryParams.Add("index_type", "physical")
		}
	}

	return fmt.Sprintf("%s?%s", logsURL, queryParams.Encode()), nil
}

// MakeLogsJSONQueryAPI posts a raw log JSON pipeline to the query_range API with the given time range.
func MakeLogsJSONQueryAPI(ctx context.Context, client *http.Client, cfg models.Config, pipeline any, startMs, endMs int64, limit int, index string) (*http.Response, error) {
	// Basic validation
	if client == nil {
		return nil, errors.New("http client cannot be nil")
	}
	if strings.TrimSpace(cfg.APIBaseURL) == "" {
		return nil, errors.New("API base URL cannot be empty")
	}
	if strings.TrimSpace(cfg.TokenManager.GetAccessToken(ctx)) == "" {
		return nil, errors.New("access token cannot be empty")
	}

	// Build URL
	logsURL := fmt.Sprintf("%s%s", cfg.APIBaseURL, constants.EndpointLogsQueryRange)
	queryParams := url.Values{}
	queryParams.Add("direction", "backward")
	queryParams.Add("start", fmt.Sprintf("%d", startMs/1000)) // seconds
	queryParams.Add("end", fmt.Sprintf("%d", endMs/1000))     // seconds
	queryParams.Add("region", cfg.Region)
	if limit > 0 {
		queryParams.Add("limit", fmt.Sprintf("%d", limit))
	}
	normalizedIndex, err := NormalizeLogIndex(index)
	if err != nil {
		return nil, err
	}
	if normalizedIndex != "" {
		queryParams.Add("index", normalizedIndex)
		if strings.HasPrefix(normalizedIndex, logIndexPhysicalPrefix) {
			queryParams.Add("index_type", "physical")
		}
	}
	fullURL := fmt.Sprintf("%s?%s", logsURL, queryParams.Encode())

	// Build body
	body := map[string]any{
		"pipeline": pipeline,
	}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal pipeline: %w", err)
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fullURL, bytes.NewBuffer(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Headers
	setServiceLogsHeaders(req, cfg.TokenManager.GetAccessToken(ctx))

	// Execute
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}

	return resp, nil
}

// createServiceLogsRequestBody creates the structured request body for service logs pipeline
func createServiceLogsRequestBody(serviceName string, severityFilters []string, bodyFilters []string) ([]byte, error) {
	if strings.TrimSpace(serviceName) == "" {
		return nil, errors.New("service name cannot be empty")
	}

	// Build the base query with service name filter
	andConditions := []map[string]any{
		{
			"$eq": []any{"ServiceName", serviceName},
		},
	}

	// Add case-insensitive severity equality filters if provided
	if len(severityFilters) > 0 {
		orConditions := make([]map[string]any, 0, len(severityFilters))
		for _, severity := range severityFilters {
			if trimmedSeverity := strings.TrimSpace(severity); trimmedSeverity != "" {
				orConditions = append(orConditions, map[string]any{
					"$ieq": []any{"SeverityText", trimmedSeverity},
				})
			}
		}

		if len(orConditions) > 0 {
			andConditions = append(andConditions, map[string]any{
				"$or": orConditions,
			})
		}
	}

	// Add case-insensitive body contains filters if provided
	if len(bodyFilters) > 0 {
		orConditions := make([]map[string]any, 0, len(bodyFilters))
		for _, bodyPattern := range bodyFilters {
			if trimmedBodyPattern := strings.TrimSpace(bodyPattern); trimmedBodyPattern != "" {
				orConditions = append(orConditions, map[string]any{
					"$icontains": []any{"Body", trimmedBodyPattern},
				})
			}
		}

		if len(orConditions) > 0 {
			andConditions = append(andConditions, map[string]any{
				"$or": orConditions,
			})
		}
	}

	pipeline := ServiceLogsRequest{
		Pipeline: []LogsPipelineStage{
			{
				Query: map[string]any{
					"$and": andConditions,
				},
				Type: "filter",
			},
		},
	}

	return json.Marshal(pipeline)
}

// setServiceLogsHeaders sets the required HTTP headers
func setServiceLogsHeaders(req *http.Request, accessToken string) {
	bearerToken := constants.BearerPrefix + accessToken
	req.Header.Set(constants.HeaderAccept, constants.HeaderAcceptJSON)
	req.Header.Set(constants.HeaderContentType, constants.HeaderContentTypeJSON)
	req.Header.Set(constants.HeaderXLast9APIToken, bearerToken)
}
