package utils

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"last9-mcp/internal/models"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/peterbourgon/ff/v3"
)

// ExtractOrgSlugFromToken extracts organization slug from JWT token
func ExtractOrgSlugFromToken(accessToken string) (string, error) {
	claims, err := ExtractClaimsFromToken(accessToken)
	if err != nil {
		return "", fmt.Errorf("failed to extract claims from token: %w", err)
	}

	orgSlug, ok := claims["organization_slug"].(string)
	if !ok {
		return "", errors.New("organization slug not found in token")
	}

	return orgSlug, nil
}

func ExtractClaimsFromToken(accessToken string) (map[string]interface{}, error) {
	// Split the token into parts
	parts := strings.Split(accessToken, ".")
	if len(parts) != 3 {
		return nil, errors.New("invalid JWT token format")
	}

	// Decode the payload (second part)
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("failed to decode token payload: %w", err)
	}

	// Parse the JSON payload
	var claims map[string]interface{}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, fmt.Errorf("failed to parse token claims: %w", err)
	}

	return claims, nil
}

func ExtractActionURLFromToken(accessToken string) (string, error) {
	// Extract ActionURL from token claims
	claims, err := ExtractClaimsFromToken(accessToken)
	if err != nil {
		return "", fmt.Errorf("failed to extract claims from token: %w", err)
	}

	// Get ActionURL from aud field
	aud, ok := claims["aud"].([]interface{})
	if !ok || len(aud) == 0 {
		return "", errors.New("no audience found in token claims")
	}

	// Handle case where audience already includes https:// protocol
	audStr := aud[0].(string)
	if strings.HasPrefix(audStr, "https://") || strings.HasPrefix(audStr, "http://") {
		return audStr, nil
	}
	return fmt.Sprintf("https://%s", audStr), nil
}

// RefreshAccessToken gets a new access token using the refresh token
func RefreshAccessToken(client *http.Client, cfg models.Config) (string, error) {
	data := map[string]string{
		"refresh_token": cfg.RefreshToken,
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}
	// Extract ActionURL from token claims
	actionURL, err := ExtractActionURLFromToken(cfg.RefreshToken)
	if err != nil {
		return "", fmt.Errorf("failed to extract action URL from refresh token: %w", err)
	}

	// Handle case where actionURL already includes /api path
	oauthURL := actionURL
	if strings.HasSuffix(actionURL, "/api") {
		oauthURL = strings.TrimSuffix(actionURL, "/api")
	}
	u, err := url.Parse(oauthURL + "/api/v4/oauth/access_token")
	if err != nil {
		return "", fmt.Errorf("failed to parse URL: %w", err)
	}

	req, err := http.NewRequest("POST", u.String(), bytes.NewReader(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var result struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	return result.AccessToken, nil
}

// GetDefaultRegion determines the region based on the Last9 BASE URL
func GetDefaultRegion(baseURL string) string {
	switch {
	case baseURL == "otlp.last9.io":
		return "us-east-1"
	case baseURL == "otlp-aps1.last9.io":
		return "ap-south-1"
	case baseURL == "otlp-apse1.last9.io":
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

// Version information
var (
	Version   = "dev"     // Set by goreleaser
	CommitSHA = "unknown" // Set by goreleaser
	BuildTime = "unknown" // Set by goreleaser
)

// setupConfig initializes and parses the configuration
func SetupConfig(defaults models.Config) (models.Config, error) {
	fs := flag.NewFlagSet("last9-mcp", flag.ExitOnError)

	var cfg models.Config
	fs.StringVar(&cfg.AuthToken, "auth", os.Getenv("LAST9_AUTH_TOKEN"), "Last9 API auth token")
	fs.StringVar(&cfg.BaseURL, "url", os.Getenv("LAST9_BASE_URL"), "Last9 API URL")
	fs.StringVar(&cfg.RefreshToken, "refresh_token", os.Getenv("LAST9_REFRESH_TOKEN"), "Last9 refresh token for authentication")
	fs.Float64Var(&cfg.RequestRateLimit, "rate", 1, "Requests per second limit")
	fs.IntVar(&cfg.RequestRateBurst, "burst", 1, "Request burst capacity")
	fs.BoolVar(&cfg.HTTPMode, "http", false, "Run as HTTP server instead of STDIO")
	fs.StringVar(&cfg.Port, "port", "8080", "HTTP server port")
	fs.StringVar(&cfg.Host, "host", "localhost", "HTTP server host")
	versionFlag := fs.Bool("version", false, "Print version information")

	var configFile string
	fs.StringVar(&configFile, "config", "", "config file path")

	err := ff.Parse(fs, os.Args[1:],
		ff.WithEnvVarPrefix("LAST9"),
		ff.WithConfigFileFlag("config"),
		ff.WithConfigFileParser(ff.JSONParser),
	)
	if err != nil {
		return cfg, fmt.Errorf("failed to parse configuration: %w", err)
	}

	if *versionFlag {
		fmt.Printf("Version: %s\nCommit: %s\nBuild Time: %s\n", Version, CommitSHA, BuildTime)
		os.Exit(0)
	}

	if cfg.AuthToken == "" {
		if defaults.AuthToken != "" {
			cfg.AuthToken = defaults.AuthToken
		} else {
			return cfg, errors.New("Last9 auth token must be provided via LAST9_AUTH_TOKEN env var")
		}
	}

	// Set default base URL if not provided
	if cfg.BaseURL == "" {
		if defaults.BaseURL != "" {
			cfg.BaseURL = defaults.BaseURL
		} else {
			return cfg, errors.New("Last9 base URL must be provided via LAST9_BASE_URL env var")
		}
	}

	if cfg.RefreshToken == "" {
		if defaults.RefreshToken != "" {
			cfg.RefreshToken = defaults.RefreshToken
		} else {
			return cfg, errors.New("Last9 refresh token must be provided via LAST9_REFRESH_TOKEN env var")
		}
	}

	return cfg, nil
}

func PopulateAPICfg(cfg *models.Config) error {
	accessToken, err := RefreshAccessToken(http.DefaultClient, *cfg)
	if err != nil {
		return fmt.Errorf("failed to refresh access token: %w", err)
	}
	cfg.AccessToken = accessToken
	orgSlug, err := ExtractOrgSlugFromToken(cfg.AccessToken)
	if err != nil {
		return fmt.Errorf("failed to extract organization slug from access token: %w", err)
	}
	cfg.OrgSlug = orgSlug
	cfg.APIBaseURL = fmt.Sprintf("https://%s/api/v4/organizations/%s", "app.last9.io", cfg.OrgSlug)
	// make a GET call to /datasources and iterate over the response array
	// find the element with is_default set to true and extract url, properties.username, properties.password
	// add bearer token auth to the request header
	req, err := http.NewRequest("GET", cfg.APIBaseURL+"/datasources", nil)
	if err != nil {
		return fmt.Errorf("failed to create request for datasources: %w", err)
	}
	req.Header.Set("X-LAST9-API-TOKEN", "Bearer "+cfg.AccessToken)
	resp, err := http.DefaultClient.Do(req)
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

func MakePromInstantAPIQuery(client *http.Client, promql string, endTimeParam int64, cfg models.Config) (*http.Response, error) {
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
	req, err := http.NewRequest("POST", reqUrl, strings.NewReader(string(bodyBytes)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-LAST9-API-TOKEN", "Bearer "+cfg.AccessToken)

	return client.Do(req)
}

func MakePromRangeAPIQuery(client *http.Client, promql string, startTimeParam, endTimeParam int64, cfg models.Config) (*http.Response, error) {
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
	req, err := http.NewRequest("POST", reqUrl, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-LAST9-API-TOKEN", "Bearer "+cfg.AccessToken)

	return client.Do(req)
}

// function to get the values of a particular label, for a given query filter
// path: /prom_label_values

func MakePromLabelValuesAPIQuery(client *http.Client, label string, matches string, startTimeParam, endTimeParam int64, cfg models.Config) (*http.Response, error) {
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
	req, err := http.NewRequest("POST", reqUrl, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-LAST9-API-TOKEN", "Bearer "+cfg.AccessToken)

	return client.Do(req)
}

func MakePromLabelsAPIQuery(client *http.Client, metric string, startTimeParam, endTimeParam int64, cfg models.Config) (*http.Response, error) {
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
	req, err := http.NewRequest("POST", reqUrl, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-LAST9-API-TOKEN", "Bearer "+cfg.AccessToken)

	return client.Do(req)
}
