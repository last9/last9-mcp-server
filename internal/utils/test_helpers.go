package utils

import (
	"os"
	"strings"
	"testing"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
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

// SetupTestConfigOrSkip creates and initializes a test configuration, or skips the test if not configured.
// This is a convenience wrapper around SetupTestConfig that handles the common skip/fail pattern.
func SetupTestConfigOrSkip(t *testing.T) *models.Config {
	t.Helper()
	cfg, err := SetupTestConfig()
	if err != nil {
		if _, ok := err.(*TestConfigError); ok {
			t.Skipf("Skipping integration test: %v", err)
		}
		t.Fatalf("failed to setup test config: %v", err)
	}
	return cfg
}

// CheckAPIError checks if an error is an API error (502, 500, etc.) and fails the test if so.
// For non-API errors, it logs a warning and returns true to indicate the test should return early.
// Returns false if there's no error and the test should continue.
func CheckAPIError(t *testing.T, err error) bool {
	t.Helper()
	if err == nil {
		return false
	}
	// Check if error is an HTTP error (like 502, 500)
	if strings.Contains(err.Error(), "status") || strings.Contains(err.Error(), "502") || strings.Contains(err.Error(), "500") {
		t.Fatalf("API returned error (test should fail): %v", err)
	}
	// For other errors, log but don't fail
	t.Logf("Integration test warning: %v", err)
	return true
}

// GetTextContent extracts TextContent from a CallToolResult, failing the test if not possible.
// Returns the text content string for further processing.
func GetTextContent(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	if len(result.Content) == 0 {
		t.Fatalf("expected content in result")
	}
	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent type")
	}
	return textContent.Text
}

