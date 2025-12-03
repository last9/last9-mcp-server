package utils

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/models"

	last9mcp "github.com/last9/mcp-go-sdk/mcp"
)

// GetDefaultRegion determines the region based on the Last9 BASE URL.
// This function extracts the hostname from URLs like "https://otlp-aps1.last9.io:443"
// and maps them to the correct AWS regions for API routing.
func GetDefaultRegion(baseURL string) string {
	// Extract hostname from URL (remove protocol and port)
	// Transform: "https://otlp-aps1.last9.io:443" → "otlp-aps1.last9.io"
	hostname := baseURL
	if strings.HasPrefix(hostname, "https://") {
		hostname = strings.TrimPrefix(hostname, "https://")
	}
	if strings.HasPrefix(hostname, "http://") {
		hostname = strings.TrimPrefix(hostname, "http://")
	}
	// Remove port if present (:443, :80, etc.)
	if colonIndex := strings.Index(hostname, ":"); colonIndex != -1 {
		hostname = hostname[:colonIndex]
	}

	switch hostname {
	case "otlp.last9.io":
		return "us-east-1"
	case "otlp-aps1.last9.io":
		return "ap-south-1"
	case "otlp-apse1.last9.io":
		return "ap-southeast-1"
	default:
		return "us-east-1" // default to us-east-1 if URL pattern doesn't match
	}
}

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
		if lookbackMinutes > 1440 { // 24 hours
			return time.Time{}, time.Time{}, fmt.Errorf("lookback_minutes cannot exceed 1440 (24 hours)")
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

	// Ensure time range doesn't exceed 24 hours
	if endTime.Sub(startTime) > 24*time.Hour {
		return time.Time{}, time.Time{}, fmt.Errorf("time range cannot exceed 24 hours")
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
	reqUrl := fmt.Sprintf("%s/prom_query_instant", cfg.APIBaseURL)
	req, err := http.NewRequestWithContext(ctx, "POST", reqUrl, strings.NewReader(string(bodyBytes)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-LAST9-API-TOKEN", "Bearer "+cfg.TokenManager.GetAccessToken(ctx))

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

	reqUrl := fmt.Sprintf("%s/prom_query", cfg.APIBaseURL)
	req, err := http.NewRequestWithContext(ctx, "POST", reqUrl, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-LAST9-API-TOKEN", "Bearer "+cfg.TokenManager.GetAccessToken(ctx))

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

	reqUrl := fmt.Sprintf("%s/prom_label_values", cfg.APIBaseURL)
	req, err := http.NewRequestWithContext(ctx, "POST", reqUrl, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-LAST9-API-TOKEN", "Bearer "+cfg.TokenManager.GetAccessToken(ctx))

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

	reqUrl := fmt.Sprintf("%s/apm/labels", cfg.APIBaseURL)
	req, err := http.NewRequestWithContext(ctx, "POST", reqUrl, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-LAST9-API-TOKEN", "Bearer "+cfg.TokenManager.GetAccessToken(ctx))

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

// ParseStringArray safely extracts string array from interface{}
func ParseStringArray(value interface{}) []string {
	var result []string
	if array, ok := value.([]interface{}); ok {
		for _, item := range array {
			if str, ok := item.(string); ok && str != "" {
				result = append(result, str)
			}
		}
	}
	return result
}

// BuildOrFilter creates an optimized filter for single or multiple values of the same field.
// For single values, returns a simple $eq filter. For multiple values, returns an $or filter.
// This optimization reduces query complexity and improves performance for single-value filters.
func BuildOrFilter(fieldName string, values []string) map[string]interface{} {
	if len(values) == 1 {
		return map[string]interface{}{
			"$eq": []interface{}{fieldName, values[0]},
		}
	}

	orConditions := make([]map[string]interface{}, 0, len(values))
	for _, value := range values {
		orConditions = append(orConditions, map[string]interface{}{
			"$eq": []interface{}{fieldName, value},
		})
	}

	return map[string]interface{}{"$or": orConditions}
}

// FetchPhysicalIndex retrieves the physical index for logs queries using the provided service name and environment
// Uses an instant query for data from the last 1 day
func FetchPhysicalIndex(ctx context.Context, client *http.Client, cfg models.Config, serviceName, env string) (string, error) {
	// Build the PromQL query with a 2-hour window
	query := fmt.Sprintf("sum by (name, destination) (physical_index_service_count{service_name='%s'", serviceName)
	if env != "" {
		query += fmt.Sprintf(",env=~'%s'", env)
	}
	query += "}[1d])"

	// Get current time for the instant query
	currentTime := time.Now().Unix()

	// Make the Prometheus instant query
	resp, err := MakePromInstantAPIQuery(ctx, client, query, currentTime, cfg)
	if err != nil {
		return "", fmt.Errorf("failed to fetch physical index: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := json.Marshal(resp.Body)
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
	tracesURL := fmt.Sprintf("%s/cat/api/traces/v2/query_range/json", cfg.APIBaseURL)
	queryParams := url.Values{}
	queryParams.Add("direction", "backward")
	queryParams.Add("start", fmt.Sprintf("%d", startMs/1000)) // seconds
	queryParams.Add("end", fmt.Sprintf("%d", endMs/1000))     // seconds
	queryParams.Add("region", GetDefaultRegion(cfg.BaseURL))
	queryParams.Add("limit", fmt.Sprintf("%d", limit))        // User-specified limit
	queryParams.Add("order", "Timestamp") // Default order
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
	bearerToken := "Bearer " + cfg.TokenManager.GetAccessToken(ctx)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", bearerToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-LAST9-API-TOKEN", bearerToken)

	// Execute
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}

	return resp, nil
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
	cfg.APIBaseURL = fmt.Sprintf("https://%s/api/v4/organizations/%s", "app.last9.io", cfg.OrgSlug)
	// make a GET call to /datasources and iterate over the response array
	// find the element with is_default set to true and extract url, properties.username, properties.password
	// add bearer token auth to the request header
	req, err := http.NewRequestWithContext(context.Background(), "GET", cfg.APIBaseURL+"/datasources", nil)
	if err != nil {
		return fmt.Errorf("failed to create request for datasources: %w", err)
	}
	req.Header.Set("X-LAST9-API-TOKEN", "Bearer "+accessToken)
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to get metrics datasource: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to get metrics datasource: %s", resp.Status)
	}
	var datasources []struct {
		IsDefault  bool   `json:"is_default"`
		URL        string `json:"url"`
		Properties struct {
			Username string `json:"username"`
			Password string `json:"password"`
		} `json:"properties"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&datasources); err != nil {
		return fmt.Errorf("failed to decode metrics datasources response: %w", err)
	}
	for _, ds := range datasources {
		if ds.IsDefault {
			cfg.PrometheusReadURL = ds.URL
			cfg.PrometheusUsername = ds.Properties.Username
			cfg.PrometheusPassword = ds.Properties.Password
			break
		}
	}
	if cfg.PrometheusReadURL == "" || cfg.PrometheusUsername == "" || cfg.PrometheusPassword == "" {
		return errors.New("default datasource not found or missing required properties")
	}
	return nil
}
