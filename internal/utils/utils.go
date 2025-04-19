package utils

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"last9-mcp/internal/models"
	"net/http"
	"net/url"
	"strings"
	"time"
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

	return fmt.Sprintf("https://%s", aud[0].(string)), nil
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

	u, err := url.Parse(actionURL + "/api/v4/oauth/access_token")
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
func GetTimeRange(params map[string]interface{}, defaultLookbackMinutes int) (startTime, endTime time.Time, err error) {
	endTime = time.Now()

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
		startTime = t

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
		endTime = t
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
