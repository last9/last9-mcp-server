package grafana

import (
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/models"
)

func testGrafanaConfig(apiBase string) models.Config {
	return models.Config{
		GrafanaAPIBaseURL: apiBase,
		TokenManager: &auth.TokenManager{
			AccessToken: "test-token",
			ExpiresAt:   time.Now().Add(24 * time.Hour),
		},
	}
}
