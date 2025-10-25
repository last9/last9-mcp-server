package testutil

import (
	"last9-mcp/internal/models"
)

// MockConfig creates a mock configuration for testing without requiring real tokens
func MockConfig() models.Config {
	return models.Config{
		BaseURL:           "https://example.com:443",
		AuthToken:         "mock-auth-token",
		RefreshToken:      "mock-refresh-token",
		AccessToken:       "mock-access-token",
		OrgSlug:           "test-org",
		APIBaseURL:        "https://example.com/api/v4/organizations/test-org",
		PrometheusReadURL: "https://example.com",
		PrometheusUsername: "test-user",
		PrometheusPassword: "test-pass",
		RequestRateLimit:  1.0,
		RequestRateBurst:  1,
	}
}
