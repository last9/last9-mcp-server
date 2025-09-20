package utils

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"last9-mcp/internal/models"
	"net/http"
	"net/url"
	"strings"
)

// Constants for service logs API
const (
	maxServiceLogsDurationHours = 24 // Maximum allowed duration for service logs

	// HTTP headers
	headerAccept          = "Accept"
	headerAuthorization   = "Authorization"
	headerContentType     = "Content-Type"
	headerContentTypeJSON = "application/json"
	headerXLast9APIToken  = "X-LAST9-API-TOKEN"
)

// ServiceLogsParams holds parameters for service logs API call
type ServiceLogsParams struct {
	Service         string
	StartTime       int64 // Unix timestamp in milliseconds
	EndTime         int64 // Unix timestamp in milliseconds
	Region          string
	SeverityFilters []string // Optional regex patterns for severity filtering
	BodyFilters     []string // Optional regex patterns for body filtering
	Index           string   // Physical index parameter for logs queries
}

func createServiceLogsParams(request ServiceLogsAPIRequest, baseURL string) ServiceLogsParams {
	return ServiceLogsParams{
		Service:         request.Service,
		StartTime:       request.StartTime,
		EndTime:         request.EndTime,
		Region:          GetDefaultRegion(baseURL),
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
	StartTime       int64    // Unix timestamp in milliseconds
	EndTime         int64    // Unix timestamp in milliseconds
	SeverityFilters []string // Optional regex patterns for severity filtering
	BodyFilters     []string // Optional regex patterns for body filtering
	Index           string   // Physical index parameter for logs queries
}

// CreateServiceLogsAPIRequest creates a new service logs API request with default options
func CreateServiceLogsAPIRequest(service string, startTime, endTime int64, severityFilters []string, bodyFilters []string, index string) ServiceLogsAPIRequest {
	return ServiceLogsAPIRequest{
		Service:         service,
		StartTime:       startTime,
		EndTime:         endTime,
		SeverityFilters: severityFilters,
		BodyFilters:     bodyFilters,
		Index:           index,
	}
}

// MakeServiceLogsAPI creates a service logs API request with improved error handling and validation
func MakeServiceLogsAPI(client *http.Client, request ServiceLogsAPIRequest, cfg *models.Config) (*http.Response, error) {
	// Validate inputs
	if err := validateServiceLogsInputs(client, cfg); err != nil {
		return nil, err
	}

	// Create parameters with dynamic region detection
	params := createServiceLogsParams(request, cfg.BaseURL)

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

	req, err := http.NewRequest(http.MethodPost, fullURL, bytes.NewBuffer(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	setServiceLogsHeaders(req, cfg.AccessToken)

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
	if strings.TrimSpace(cfg.AccessToken) == "" {
		return errors.New("access token cannot be empty")
	}
	return nil
}

// buildServiceLogsURL constructs the full URL with query parameters for service logs API
func buildServiceLogsURL(apiBaseURL string, params ServiceLogsParams) (string, error) {
	if strings.TrimSpace(apiBaseURL) == "" {
		return "", errors.New("API base URL cannot be empty")
	}

	logsURL := fmt.Sprintf("%s/logs/api/v2/query_range/json", apiBaseURL)

	queryParams := url.Values{}
	queryParams.Add("direction", "backward")
	queryParams.Add("start", fmt.Sprintf("%d", params.StartTime/1000))  // Convert to seconds
	queryParams.Add("end", fmt.Sprintf("%d", params.EndTime/1000))      // Convert to seconds
	queryParams.Add("region", params.Region)

	// Add index parameter if provided
	if params.Index != "" {
		queryParams.Add("index", params.Index)
		// For physical indexes, we might need index_type=physical
		if strings.HasPrefix(params.Index, "physical_index:") {
			queryParams.Add("index_type", "physical")
		}
	}

	return fmt.Sprintf("%s?%s", logsURL, queryParams.Encode()), nil
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

	// Add severity regex filters if provided
	if len(severityFilters) > 0 {
		orConditions := make([]map[string]any, 0, len(severityFilters))
		for _, severity := range severityFilters {
			if strings.TrimSpace(severity) != "" {
				// Use case insensitive regex with (?i) flag
				caseInsensitivePattern := "(?i)" + severity
				orConditions = append(orConditions, map[string]any{
					"$regex": []any{"SeverityText", caseInsensitivePattern},
				})
			}
		}

		if len(orConditions) > 0 {
			andConditions = append(andConditions, map[string]any{
				"$or": orConditions,
			})
		}
	}

	// Add body regex filters if provided
	if len(bodyFilters) > 0 {
		orConditions := make([]map[string]any, 0, len(bodyFilters))
		for _, bodyPattern := range bodyFilters {
			if strings.TrimSpace(bodyPattern) != "" {
				// Use case insensitive regex with (?i) flag for contains matching
				caseInsensitivePattern := "(?i)" + bodyPattern
				orConditions = append(orConditions, map[string]any{
					"$regex": []any{"Body", caseInsensitivePattern},
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
	bearerToken := "Bearer " + accessToken
	req.Header.Set(headerAccept, headerContentTypeJSON)
	req.Header.Set(headerAuthorization, bearerToken)
	req.Header.Set(headerContentType, headerContentTypeJSON)
	req.Header.Set(headerXLast9APIToken, bearerToken)
}
