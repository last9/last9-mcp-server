package utils

import (
	"os"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/models"
)

// SetupTestConfig creates and initializes a test configuration.
// It automatically reads TEST_REFRESH_TOKEN and TEST_DATASOURCE from environment variables.
// Returns an error if TEST_REFRESH_TOKEN is not set or if PopulateAPICfg fails.
func SetupTestConfig() (*models.Config, error) {
	testRefreshToken := os.Getenv("TEST_REFRESH_TOKEN")
	if testRefreshToken == "" {
		return nil, &TestConfigError{Message: "TEST_REFRESH_TOKEN not set"}
	}

	cfg := models.Config{
		RefreshToken:   testRefreshToken,
		DatasourceName: os.Getenv("TEST_DATASOURCE"), // Automatically reads from environment
	}

	tokenManager, err := auth.NewTokenManager(testRefreshToken)
	if err != nil {
		return nil, err
	}
	cfg.TokenManager = tokenManager

	if err := PopulateAPICfg(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// TestConfigError represents an error in test configuration setup
type TestConfigError struct {
	Message string
}

func (e *TestConfigError) Error() string {
	return e.Message
}

