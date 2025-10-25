package utils

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"last9-mcp/internal/models"
)

func TestMakeLogsJSONQueryAPI(t *testing.T) {
	if os.Getenv("ENABLE_INTEGRATION_TESTS") == "" {
		t.Skip("Skipping integration test - set ENABLE_INTEGRATION_TESTS=1 to run")
	}

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

	// Build config from environment variables
	cfg := models.Config{
		BaseURL:      os.Getenv("LAST9_BASE_URL"),
		AuthToken:    os.Getenv("LAST9_AUTH_TOKEN"),
		RefreshToken: os.Getenv("LAST9_REFRESH_TOKEN"),
	}
	if err := PopulateAPICfg(&cfg); err != nil {
		t.Fatalf("failed to populate API config: %v", err)
	}

	ctx := context.Background()
	// Execute
	resp, err := MakeLogsJSONQueryAPI(ctx, http.DefaultClient, cfg, pipeline, start, end)
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
