package utils

import (
	"errors"
	"math/rand/v2"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/joho/godotenv"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var loadTestEnvOnce sync.Once

// sharedTestConfig is initialized exactly once per test process to avoid
// hammering the token endpoint when many integration tests run in parallel.
var (
	sharedTestCfg     *models.Config
	sharedTestCfgErr  error
	sharedTestCfgOnce sync.Once
)

func loadTestEnv() {
	loadTestEnvOnce.Do(func() {
		dir, err := os.Getwd()
		if err != nil {
			return
		}
		for {
			candidate := filepath.Join(dir, ".env")
			if _, err := os.Stat(candidate); err == nil {
				_ = godotenv.Load(candidate)
				return
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	})
}

func resolveTestRefreshToken() string {
	loadTestEnv()
	if t := os.Getenv("TEST_REFRESH_TOKEN"); t != "" {
		return t
	}
	return os.Getenv("LAST9_REFRESH_TOKEN")
}

// SetupTestConfig creates and initializes a test configuration.
// It loads .env (if present), then reads TEST_REFRESH_TOKEN or LAST9_REFRESH_TOKEN,
// and TEST_DATASOURCE from environment variables.
// Returns an error if no refresh token is set or if PopulateAPICfg fails.
//
// The underlying TokenManager is shared across the entire test process so that
// parallel integration tests do not all race to exchange the refresh token at
// startup (which would trigger 429 rate-limit errors on the auth endpoint).
func SetupTestConfig() (*models.Config, error) {
	sharedTestCfgOnce.Do(func() {
		testRefreshToken := resolveTestRefreshToken()
		if testRefreshToken == "" {
			sharedTestCfgErr = &TestConfigError{Message: "TEST_REFRESH_TOKEN or LAST9_REFRESH_TOKEN not set"}
			return
		}

		cfg := models.Config{
			RefreshToken:   testRefreshToken,
			DatasourceName: os.Getenv("TEST_DATASOURCE"),
		}

		// Retry up to 5 times on 429 (rate limit). All package binaries race to
		// exchange the refresh token when "go test ./..." starts them in parallel;
		// a randomised initial delay + exponential back-off avoids a thundering-herd
		// on the auth endpoint.
		const maxAttempts = 5
		// Initial per-binary jitter (0–2 s) staggers the first attempts.
		time.Sleep(time.Duration(rand.N(2000)) * time.Millisecond)
		var tokenManager *auth.TokenManager
		for attempt := range maxAttempts {
			var err error
			tokenManager, err = auth.NewTokenManager(testRefreshToken)
			if err == nil {
				break
			}
			var httpErr *auth.HTTPError
			if !errors.As(err, &httpErr) || httpErr.StatusCode != http.StatusTooManyRequests {
				sharedTestCfgErr = err
				return
			}
			if attempt == maxAttempts-1 {
				sharedTestCfgErr = &rateLimitExhaustedError{}
				return
			}
			// Exponential back-off: 2s, 4s, 8s, 16s + up to 1 s jitter.
			backoff := time.Duration(2<<uint(attempt))*time.Second +
				time.Duration(rand.N(1000))*time.Millisecond
			time.Sleep(backoff)
		}
		cfg.TokenManager = tokenManager

		if err := PopulateAPICfg(&cfg); err != nil {
			sharedTestCfgErr = err
			return
		}

		sharedTestCfg = &cfg
	})

	if sharedTestCfgErr != nil {
		return nil, sharedTestCfgErr
	}
	// Return a shallow copy so callers can mutate fields (e.g. DatasourceName)
	// without affecting other tests.
	cfgCopy := *sharedTestCfg
	return &cfgCopy, nil
}

// TestConfigError represents an error in test configuration setup
type TestConfigError struct {
	Message string
}

func (e *TestConfigError) Error() string {
	return e.Message
}

// rateLimitExhaustedError is returned when token exchange retries are all
// exhausted due to 429 responses. Tests treat this as a skip, not a failure.
type rateLimitExhaustedError struct{}

func (e *rateLimitExhaustedError) Error() string {
	return "auth endpoint rate-limited; token exchange retries exhausted"
}

// SetupTestConfigOrSkip creates and initializes a test configuration, or skips the test if not configured.
// This is a convenience wrapper around SetupTestConfig that handles the common skip/fail pattern.
func SetupTestConfigOrSkip(t *testing.T) *models.Config {
	t.Helper()
	cfg, err := SetupTestConfig()
	if err != nil {
		switch err.(type) {
		case *TestConfigError, *rateLimitExhaustedError:
			t.Skipf("Skipping integration test: %v", err)
		}
		t.Fatalf("failed to setup test config: %v", err)
	}
	return cfg
}

// SetupTestConfigWithTokenOrSkip builds a config using the given env var as the refresh token.
// Falls back to fallback if the env var is not set (allows the caller to proceed with reduced permissions).
func SetupTestConfigWithTokenOrSkip(t *testing.T, envVar string, fallback *models.Config) *models.Config {
	t.Helper()
	if fallback == nil {
		t.Fatalf("fallback config must not be nil")
	}
	loadTestEnv()
	token := os.Getenv(envVar)
	if token == "" {
		t.Logf("%s not set; using default token (some operations may be permission-limited)", envVar)
		return fallback
	}
	cfg := models.Config{
		RefreshToken:   token,
		DatasourceName: fallback.DatasourceName,
	}
	tokenManager, err := auth.NewTokenManager(token)
	if err != nil {
		t.Fatalf("failed to create token manager from %s: %v", envVar, err)
	}
	cfg.TokenManager = tokenManager
	if err := PopulateAPICfg(&cfg); err != nil {
		t.Fatalf("failed to populate API config from %s: %v", envVar, err)
	}
	return &cfg
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

