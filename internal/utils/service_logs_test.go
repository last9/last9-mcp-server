package utils

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"last9-mcp/internal/models"
)

func TestMakeLogsJSONQueryAPI(t *testing.T) {
	// Prepare a fake pipeline
	pipeline := []map[string]any{
		{
			"type":  "filter",
			"query": map[string]any{"$contains": []any{"Body", "a"}},
		},
		{
			"type":  "select",
			"limit": 20,
		},
	}

	// Use current time for end (milliseconds)
	end := time.Now().UnixMilli()
	start := end - int64(60*time.Minute/time.Millisecond)

	// Build config
	// Mirror env usage from apm_test.go
	baseURL := os.Getenv("TEST_BASE_URL")
	authToken := os.Getenv("TEST_AUTH_TOKEN")
	refreshToken := os.Getenv("TEST_REFRESH_TOKEN")

	cfg := models.Config{
		BaseURL:      baseURL,
		AuthToken:    authToken,
		RefreshToken: refreshToken,
	}
	// Populate API cfg (e.g., AccessToken); then override APIBaseURL to our test server
	if err := PopulateAPICfg(&cfg); err != nil {
		t.Fatalf("failed to populate API cfg: %v", err)
	}

	// Execute
	resp, err := MakeLogsJSONQueryAPI(http.DefaultClient, cfg, pipeline, start, end)
	if err != nil {
		t.Fatalf("MakeLogsJSONQueryAPI returned error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
	fmt.Println(resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}
	fmt.Println(string(body))
}
