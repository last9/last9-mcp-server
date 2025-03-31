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
)

// ExtractOrgSlugFromToken extracts organization slug from JWT token
func ExtractOrgSlugFromToken(accessToken string) (string, error) {
	// Split the token into parts
	parts := strings.Split(accessToken, ".")
	if len(parts) != 3 {
		return "", errors.New("invalid JWT token format")
	}

	// Decode the payload (second part)
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", fmt.Errorf("failed to decode token payload: %w", err)
	}

	// Parse the JSON payload
	var claims struct {
		OrganizationSlug string `json:"organization_slug"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", fmt.Errorf("failed to parse token claims: %w", err)
	}

	if claims.OrganizationSlug == "" {
		return "", errors.New("organization_slug not found in token")
	}

	return claims.OrganizationSlug, nil
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

	u, err := url.Parse(cfg.ActionURL + "/api/v4/oauth/access_token")
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
