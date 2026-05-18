package dashboards

import (
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/models"
)

func testDashboardConfig(apiBase string) models.Config {
	return models.Config{
		APIBaseURL: apiBase,
		OrgSlug:    "test-org",
		Region:     "us-east-1",
		ClusterID:  "cluster-1",
		TokenManager: &auth.TokenManager{
			AccessToken: "test-token",
			ExpiresAt:   time.Now().Add(24 * time.Hour),
		},
	}
}
