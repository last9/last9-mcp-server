package utils

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/constants"
	"last9-mcp/internal/models"

	last9mcp "github.com/last9/mcp-go-sdk/mcp"
)

// Constants for time-related values
const (
	// MaxLookbackMinutes is the maximum number of minutes allowed for lookback queries (24 hours)
	MaxLookbackMinutes = 1440
	// MaxTimeRangeHours is the maximum time range allowed for queries (24 hours)
	MaxTimeRangeHours = 24
	// DefaultLookbackMinutes is the default lookback time in minutes (1 hour)
	DefaultLookbackMinutes = 60
	// DefaultHTTPTimeout is the default HTTP client timeout
	DefaultHTTPTimeout = 30 * time.Second
	// TokenRefreshBuffer is the percentage of token lifetime to refresh before expiry (50%)
	TokenRefreshBufferPercent = 50
)

// GetTimeRange returns start and end times based on lookback minutes
// If start_time_iso and end_time_iso are provided, they take precedence
// Otherwise, returns now and now - lookbackMinutes
//
// IMPORTANT: All ISO timestamps are parsed as UTC to ensure consistent behavior
// across different server timezones. This prevents timezone-related bugs where
// queries return data from unexpected time periods.
func GetTimeRange(params map[string]interface{}, defaultLookbackMinutes int) (startTime, endTime time.Time, err error) {
	// Always use UTC to ensure consistent behavior across timezones
	endTime = time.Now().UTC()

	// First check if lookback_minutes is provided
	lookbackMinutes := defaultLookbackMinutes
	if l, ok := params["lookback_minutes"].(float64); ok {
		lookbackMinutes = int(l)
		if lookbackMinutes < 1 {
			return time.Time{}, time.Time{}, fmt.Errorf("lookback_minutes must be at least 1")
		}
		if lookbackMinutes > MaxLookbackMinutes {
			return time.Time{}, time.Time{}, fmt.Errorf("lookback_minutes cannot exceed %d (24 hours)", MaxLookbackMinutes)
		}
	}

	// Default start time based on lookback
	startTime = endTime.Add(time.Duration(-lookbackMinutes) * time.Minute)

	// Override with explicit timestamps if provided
	if startTimeStr, ok := params["start_time_iso"].(string); ok && startTimeStr != "" {
		t, err := time.Parse("2006-01-02 15:04:05", startTimeStr)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid start_time_iso format: %w", err)
		}
		// Force UTC timezone to prevent server timezone from affecting timestamp interpretation
		// This ensures "2025-06-23 16:00:00" is always treated as UTC, not local time
		startTime = time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), time.UTC)

		// If start_time is provided but no end_time, use start_time + lookback_minutes
		if endTimeStr, ok := params["end_time_iso"].(string); !ok || endTimeStr == "" {
			endTime = startTime.Add(time.Duration(lookbackMinutes) * time.Minute)
		}
	}

	if endTimeStr, ok := params["end_time_iso"].(string); ok && endTimeStr != "" {
		t, err := time.Parse("2006-01-02 15:04:05", endTimeStr)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid end_time_iso format: %w", err)
		}
		// Force UTC timezone to prevent server timezone from affecting timestamp interpretation
		// This ensures "2025-06-23 16:30:00" is always treated as UTC, not local time
		endTime = time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), time.UTC)
	}

	// Validate time range
	if startTime.After(endTime) {
		return time.Time{}, time.Time{}, fmt.Errorf("start_time cannot be after end_time")
	}

	// Ensure time range doesn't exceed maximum allowed
	if endTime.Sub(startTime) > MaxTimeRangeHours*time.Hour {
		return time.Time{}, time.Time{}, fmt.Errorf("time range cannot exceed %d hours", MaxTimeRangeHours)
	}

	return startTime, endTime, nil
}

func MakePromInstantAPIQuery(ctx context.Context, client *http.Client, promql string, endTimeParam int64, cfg models.Config) (*http.Response, error) {
	promInstantParam := struct {
		Query     string `json:"query"`
		Timestamp int64  `json:"timestamp"`
		ReadURL   string `json:"read_url"`
		Username  string `json:"username"`
		Password  string `json:"password"`
	}{promql, endTimeParam, cfg.PrometheusReadURL, cfg.PrometheusUsername, cfg.PrometheusPassword}
	bodyBytes, err := json.Marshal(promInstantParam)
	if err != nil {
		return nil, err
	}
	reqUrl := fmt.Sprintf("%s%s", cfg.APIBaseURL, constants.EndpointPromQueryInstant)
	req, err := http.NewRequestWithContext(ctx, "POST", reqUrl, strings.NewReader(string(bodyBytes)))
	if err != nil {
		return nil, err
	}
	req.Header.Set(constants.HeaderContentType, constants.HeaderContentTypeJSON)
	req.Header.Set(constants.HeaderXLast9APIToken, constants.BearerPrefix+cfg.TokenManager.GetAccessToken(ctx))

	return client.Do(req)
}

func MakePromRangeAPIQuery(ctx context.Context, client *http.Client, promql string, startTimeParam, endTimeParam int64, cfg models.Config) (*http.Response, error) {
	promRangeParam := struct {
		Query     string `json:"query"`
		Timestamp int64  `json:"timestamp"`
		Window    int64  `json:"window"`
		ReadURL   string `json:"read_url"`
		Username  string `json:"username"`
		Password  string `json:"password"`
	}{
		Query:     promql,
		Timestamp: startTimeParam,
		Window:    endTimeParam - startTimeParam,
		ReadURL:   cfg.PrometheusReadURL,
		Username:  cfg.PrometheusUsername,
		Password:  cfg.PrometheusPassword,
	}

	bodyBytes, err := json.Marshal(promRangeParam)
	if err != nil {
		return nil, err
	}

	reqUrl := fmt.Sprintf("%s%s", cfg.APIBaseURL, constants.EndpointPromQuery)
	req, err := http.NewRequestWithContext(ctx, "POST", reqUrl, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set(constants.HeaderContentType, constants.HeaderContentTypeJSON)
	req.Header.Set(constants.HeaderXLast9APIToken, constants.BearerPrefix+cfg.TokenManager.GetAccessToken(ctx))

	return client.Do(req)
}

// function to get the values of a particular label, for a given query filter
// path: /prom_label_values

func MakePromLabelValuesAPIQuery(ctx context.Context, client *http.Client, label string, matches string, startTimeParam, endTimeParam int64, cfg models.Config) (*http.Response, error) {
	promLabelValuesParam := struct {
		Label     string   `json:"label"`
		Timestamp int64    `json:"timestamp"`
		Window    int64    `json:"window"`
		ReadURL   string   `json:"read_url"`
		Username  string   `json:"username"`
		Password  string   `json:"password"`
		Matches   []string `json:"matches"`
	}{
		Label:     label,
		Timestamp: startTimeParam,
		Window:    endTimeParam - startTimeParam,
		ReadURL:   cfg.PrometheusReadURL,
		Username:  cfg.PrometheusUsername,
		Password:  cfg.PrometheusPassword,
		Matches:   []string{matches},
	}

	bodyBytes, err := json.Marshal(promLabelValuesParam)
	if err != nil {
		return nil, err
	}

	reqUrl := fmt.Sprintf("%s%s", cfg.APIBaseURL, constants.EndpointPromLabelValues)
	req, err := http.NewRequestWithContext(ctx, "POST", reqUrl, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set(constants.HeaderContentType, constants.HeaderContentTypeJSON)
	req.Header.Set(constants.HeaderXLast9APIToken, constants.BearerPrefix+cfg.TokenManager.GetAccessToken(ctx))

	return client.Do(req)
}

func MakePromLabelsAPIQuery(ctx context.Context, client *http.Client, metric string, startTimeParam, endTimeParam int64, cfg models.Config) (*http.Response, error) {
	promLabelsParam := struct {
		Timestamp int64  `json:"timestamp"`
		Window    int64  `json:"window"`
		ReadURL   string `json:"read_url"`
		Username  string `json:"username"`
		Password  string `json:"password"`
		Metric    string `json:"metric"`
	}{
		Timestamp: startTimeParam,
		Window:    endTimeParam - startTimeParam,
		ReadURL:   cfg.PrometheusReadURL,
		Username:  cfg.PrometheusUsername,
		Password:  cfg.PrometheusPassword,
		Metric:    metric,
	}

	bodyBytes, err := json.Marshal(promLabelsParam)
	if err != nil {
		return nil, err
	}

	reqUrl := fmt.Sprintf("%s%s", cfg.APIBaseURL, constants.EndpointAPMLabels)
	req, err := http.NewRequestWithContext(ctx, "POST", reqUrl, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set(constants.HeaderContentType, constants.HeaderContentTypeJSON)
	req.Header.Set(constants.HeaderXLast9APIToken, constants.BearerPrefix+cfg.TokenManager.GetAccessToken(ctx))

	return client.Do(req)
}

// ConvertTimestamp converts a timestamp from the API response to RFC3339 format
func ConvertTimestamp(timestamp any) string {
	switch ts := timestamp.(type) {
	case string:
		// Try to parse as Unix nanoseconds timestamp
		if nsTimestamp, err := strconv.ParseInt(ts, 10, 64); err == nil {
			// Convert nanoseconds to time.Time and format as RFC3339
			return time.Unix(0, nsTimestamp).UTC().Format(time.RFC3339)
		}
		// If it's already a formatted timestamp, return as is
		return ts
	case float64:
		// Convert to int64 and treat as nanoseconds
		return time.Unix(0, int64(ts)).UTC().Format(time.RFC3339)
	case int64:
		// Treat as nanoseconds
		return time.Unix(0, ts).UTC().Format(time.RFC3339)
	default:
		// Fallback to current time if we can't parse
		return time.Now().UTC().Format(time.RFC3339)
	}
}

// FetchPhysicalIndex retrieves the physical index for logs queries using the provided service name and environment
func FetchPhysicalIndex(ctx context.Context, client *http.Client, cfg models.Config, serviceName, env string) (string, error) {
	query := fmt.Sprintf("sum by (name, destination) (physical_index_service_count{service_name='%s'", serviceName)
	if env != "" {
		query += fmt.Sprintf(",env=~'%s'", env)
	}
	query += "}[1d])"

	currentTime := time.Now().Unix()
	resp, err := MakePromInstantAPIQuery(ctx, client, query, currentTime, cfg)
	if err != nil {
		return "", fmt.Errorf("failed to fetch physical index: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("physical index API returned status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	// Parse the response to extract the first index
	var physicalIndexResponse []struct {
		Metric map[string]string `json:"metric"`
		Value  []interface{}     `json:"value"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&physicalIndexResponse); err != nil {
		return "", fmt.Errorf("failed to decode physical index response: %w", err)
	}

	if len(physicalIndexResponse) == 0 {
		// Continue without index if it is not available
		return "", nil
	}

	// Extract the index name from the first result
	firstResult := physicalIndexResponse[0]

	if indexName, exists := firstResult.Metric["name"]; exists {
		return fmt.Sprintf("physical_index:%s", indexName), nil
	}

	return "", fmt.Errorf("no index name found in physical index response")
}

// MakeTracesJSONQueryAPI posts a raw trace JSON pipeline to the query_range API with the given time range
func MakeTracesJSONQueryAPI(ctx context.Context, client *http.Client, cfg models.Config, pipeline any, startMs, endMs int64, limit int) (*http.Response, error) {
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

	// Validate and set default limit
	if limit <= 0 {
		limit = 20 // Default limit
	}
	if limit > 100 {
		limit = 100 // Maximum reasonable limit
	}

	// Build URL
	tracesURL := fmt.Sprintf("%s%s", cfg.APIBaseURL, constants.EndpointTracesQueryRange)
	queryParams := url.Values{}
	queryParams.Add("direction", "backward")
	queryParams.Add("start", fmt.Sprintf("%d", startMs/1000)) // seconds
	queryParams.Add("end", fmt.Sprintf("%d", endMs/1000))     // seconds
	queryParams.Add("region", cfg.Region)
	queryParams.Add("limit", fmt.Sprintf("%d", limit)) // User-specified limit
	queryParams.Add("order", "Timestamp")              // Default order
	fullURL := fmt.Sprintf("%s?%s", tracesURL, queryParams.Encode())

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
	bearerToken := constants.BearerPrefix + cfg.TokenManager.GetAccessToken(ctx)
	req.Header.Set(constants.HeaderAccept, constants.HeaderAcceptJSON)
	req.Header.Set(constants.HeaderContentType, constants.HeaderContentTypeJSON)
	req.Header.Set(constants.HeaderXLast9APIToken, bearerToken)

	// Execute
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed (URL: %s): %w", fullURL, err)
	}

	return resp, nil
}

// Datasource represents a datasource configuration
type Datasource struct {
	Name       string `json:"name"`
	IsDefault  bool   `json:"is_default"`
	URL        string `json:"url"`
	Region     string `json:"region"`
	Properties struct {
		Username string `json:"username"`
		Password string `json:"password"`
	} `json:"properties"`
}

// GetDatasourceByName fetches all datasources and returns the one matching the provided name.
// If datasourceName is empty, returns nil (indicating to use default).
// Returns an error if the datasource is not found.
func GetDatasourceByName(ctx context.Context, client *http.Client, cfg models.Config, datasourceName string) (*Datasource, error) {
	if datasourceName == "" {
		return nil, nil // Use default
	}

	// Fetch all datasources
	req, err := http.NewRequestWithContext(ctx, "GET", cfg.APIBaseURL+constants.EndpointDatasources, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request for datasources: %w", err)
	}
	req.Header.Set(constants.HeaderXLast9APIToken, constants.BearerPrefix+cfg.TokenManager.GetAccessToken(ctx))

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get datasources: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to get datasources: status %d: %s", resp.StatusCode, string(body))
	}

	var datasources []Datasource
	if err := json.NewDecoder(resp.Body).Decode(&datasources); err != nil {
		return nil, fmt.Errorf("failed to decode datasources response: %w", err)
	}

	// Find the datasource matching the provided name
	for _, ds := range datasources {
		if ds.Name == datasourceName {
			return &ds, nil
		}
	}

	return nil, fmt.Errorf("datasource with name '%s' not found", datasourceName)
}

// PopulateAPICfg populates the API configuration with necessary details
func PopulateAPICfg(cfg *models.Config) error {
	if cfg.TokenManager == nil {
		return errors.New("TokenManager is required but not initialized")
	}

	accessToken := cfg.TokenManager.GetAccessToken(context.Background())
	orgSlug, err := auth.ExtractOrgSlugFromToken(accessToken)
	if err != nil {
		return fmt.Errorf("failed to extract org slug from token: %w", err)
	}
	cfg.OrgSlug = orgSlug

	actionURL, err := auth.ExtractActionURLFromToken(accessToken)
	if err != nil {
		return fmt.Errorf("failed to extract action URL from token: %w", err)
	}
	cfg.ActionURL = actionURL

	client := last9mcp.WithHTTPTracing(&http.Client{Timeout: 30 * time.Second})
	cfg.APIBaseURL = fmt.Sprintf("https://%s/api/v4/organizations/%s", constants.APIBaseHost, cfg.OrgSlug)
	req, err := http.NewRequestWithContext(context.Background(), "GET", cfg.APIBaseURL+constants.EndpointDatasources, nil)
	if err != nil {
		return fmt.Errorf("failed to create request for datasources: %w", err)
	}
	req.Header.Set(constants.HeaderXLast9APIToken, constants.BearerPrefix+accessToken)
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to get metrics datasource: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to get metrics datasource: %s", resp.Status)
	}
	var datasourcesList []Datasource
	if err := json.NewDecoder(resp.Body).Decode(&datasourcesList); err != nil {
		return fmt.Errorf("failed to decode metrics datasources response: %w", err)
	}

	// Find the datasource to use
	var selectedDatasource *Datasource
	if cfg.DatasourceName != "" {
		// Use specified datasource by name
		for i := range datasourcesList {
			if datasourcesList[i].Name == cfg.DatasourceName {
				selectedDatasource = &datasourcesList[i]
				break
			}
		}
		if selectedDatasource == nil {
			return fmt.Errorf("datasource with name '%s' not found", cfg.DatasourceName)
		}
	} else {
		// Use default datasource
		for i := range datasourcesList {
			if datasourcesList[i].IsDefault {
				selectedDatasource = &datasourcesList[i]
				break
			}
		}
		if selectedDatasource == nil {
			return errors.New("default datasource not found")
		}
	}

	// Set config from selected datasource
	cfg.PrometheusReadURL = selectedDatasource.URL
	cfg.PrometheusUsername = selectedDatasource.Properties.Username
	cfg.PrometheusPassword = selectedDatasource.Properties.Password
	cfg.Region = selectedDatasource.Region

	if cfg.PrometheusReadURL == "" || cfg.PrometheusUsername == "" || cfg.PrometheusPassword == "" || cfg.Region == "" {
		return errors.New("selected datasource missing required properties")
	}
	return nil
}
