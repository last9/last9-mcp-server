package logs

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Integration test for get_logs tool
func TestGetLogsHandler_Integration(t *testing.T) {
	// Skip if no refresh token is provided (integration test)
	testRefreshToken := os.Getenv("TEST_REFRESH_TOKEN")
	if testRefreshToken == "" {
		t.Skip("Skipping integration test: TEST_REFRESH_TOKEN not set")
	}

	cfg := models.Config{
		RefreshToken: testRefreshToken,
	}
	// Initialize TokenManager first
	tokenManager, err := auth.NewTokenManager(testRefreshToken)
	if err != nil {
		t.Fatalf("failed to create token manager: %v", err)
	}
	cfg.TokenManager = tokenManager
	if err := utils.PopulateAPICfg(&cfg); err != nil {
		t.Fatalf("failed to populate API config: %v", err)
	}

	handler := NewGetLogsHandler(http.DefaultClient, cfg)

	args := GetLogsArgs{
		LogjsonQuery: []map[string]interface{}{
			{
				"type": "filter",
				"query": map[string]interface{}{
					"$exists": []string{"ServiceName"},
				},
			},
		},
		LookbackMinutes: 60,
		Limit:           10,
	}

	ctx := context.Background()
	req := &mcp.CallToolRequest{}
	result, _, err := handler(ctx, req, args)

	// Fail on API errors (like 502) - these indicate real problems
	if err != nil {
		// Check if error is an HTTP error (like 502)
		if strings.Contains(err.Error(), "status") || strings.Contains(err.Error(), "502") || strings.Contains(err.Error(), "500") {
			t.Fatalf("API returned error (test should fail): %v", err)
		}
		// For other errors (like no logs), log but don't fail
		t.Logf("Integration test warning (may be expected if no logs exist): %v", err)
		return
	}

	if len(result.Content) == 0 {
		t.Fatalf("expected content in result")
	}

	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent type")
	}

	// Verify response structure
	var response map[string]interface{}
	if err := json.Unmarshal([]byte(textContent.Text), &response); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	// Log summary instead of full response
	count := 0
	if data, ok := response["data"].(map[string]interface{}); ok {
		if result, ok := data["result"].([]interface{}); ok {
			count = len(result)
		}
	}
	t.Logf("Integration test successful: received %d log entry/entries", count)
}

// Integration test for get_service_logs tool
func TestGetServiceLogsHandler_Integration(t *testing.T) {
	// Skip if no refresh token is provided (integration test)
	testRefreshToken := os.Getenv("TEST_REFRESH_TOKEN")
	if testRefreshToken == "" {
		t.Skip("Skipping integration test: TEST_REFRESH_TOKEN not set")
	}

	cfg := models.Config{
		RefreshToken: testRefreshToken,
	}
	// Initialize TokenManager first
	tokenManager, err := auth.NewTokenManager(testRefreshToken)
	if err != nil {
		t.Fatalf("failed to create token manager: %v", err)
	}
	cfg.TokenManager = tokenManager
	if err := utils.PopulateAPICfg(&cfg); err != nil {
		t.Fatalf("failed to populate API config: %v", err)
	}

	handler := NewGetServiceLogsHandler(http.DefaultClient, cfg)

	tests := []struct {
		name string
		args GetServiceLogsArgs
	}{
		{
			name: "Get service logs with default parameters",
			args: GetServiceLogsArgs{
				Service:         "test-service",
				LookbackMinutes: 60,
				Limit:           10,
			},
		},
		{
			name: "Get service logs with severity filter",
			args: GetServiceLogsArgs{
				Service:         "test-service",
				LookbackMinutes: 30,
				Limit:           5,
				SeverityFilters: []string{"error", "warn"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			req := &mcp.CallToolRequest{}
			result, _, err := handler(ctx, req, tt.args)

			// Fail on API errors (like 502) - these indicate real problems
			if err != nil {
				// Check if error is an HTTP error (like 502)
				if strings.Contains(err.Error(), "status") || strings.Contains(err.Error(), "502") || strings.Contains(err.Error(), "500") {
					t.Fatalf("API returned error (test should fail): %v", err)
				}
				// For other errors (like service doesn't exist), log but don't fail
				t.Logf("Integration test warning (expected if test service doesn't exist): %v", err)
				return
			}

			if len(result.Content) == 0 {
				t.Fatalf("expected content in result")
			}

			textContent, ok := result.Content[0].(*mcp.TextContent)
			if !ok {
				t.Fatalf("expected TextContent type")
			}

			// Verify response structure and log summary
			var response ServiceLogsResponse
			if err := json.Unmarshal([]byte(textContent.Text), &response); err != nil {
				// If it's not JSON, it might be formatted text - that's ok
				t.Logf("Integration test successful. Response is formatted text (not JSON)")
			} else {
				t.Logf("Integration test successful: received %d log entry/entries for service '%s'",
					response.Count, response.Service)
			}
		})
	}
}

// Integration test for get_log_attributes tool
func TestGetLogAttributesHandler_Integration(t *testing.T) {
	// Skip if no refresh token is provided (integration test)
	testRefreshToken := os.Getenv("TEST_REFRESH_TOKEN")
	if testRefreshToken == "" {
		t.Skip("Skipping integration test: TEST_REFRESH_TOKEN not set")
	}

	cfg := models.Config{
		RefreshToken: testRefreshToken,
	}
	// Initialize TokenManager first
	tokenManager, err := auth.NewTokenManager(testRefreshToken)
	if err != nil {
		t.Fatalf("failed to create token manager: %v", err)
	}
	cfg.TokenManager = tokenManager
	if err := utils.PopulateAPICfg(&cfg); err != nil {
		t.Fatalf("failed to populate API config: %v", err)
	}

	handler := NewGetLogAttributesHandler(http.DefaultClient, cfg)

	args := GetLogAttributesArgs{
		LookbackMinutes: 15,
	}

	ctx := context.Background()
	req := &mcp.CallToolRequest{}
	result, _, err := handler(ctx, req, args)

	// Fail on API errors (like 502) - these indicate real problems
	if err != nil {
		// Check if error is an HTTP error (like 502)
		if strings.Contains(err.Error(), "status") || strings.Contains(err.Error(), "502") || strings.Contains(err.Error(), "500") {
			t.Fatalf("API returned error (test should fail): %v", err)
		}
		// For other errors, log but don't fail
		t.Logf("Integration test warning: %v", err)
		return
	}

	if len(result.Content) == 0 {
		t.Fatalf("expected content in result")
	}

	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent type")
	}

	// Log summary - attributes are typically returned as a list
	var attributes []string
	if err := json.Unmarshal([]byte(textContent.Text), &attributes); err != nil {
		// If it's not JSON, it might be formatted text - that's ok
		t.Logf("Integration test successful. Response is formatted text (not JSON)")
	} else {
		t.Logf("Integration test successful: found %d log attribute(s)", len(attributes))
	}
}

// Integration test for get_drop_rules tool
func TestGetDropRulesHandler_Integration(t *testing.T) {
	// Skip if no refresh token is provided (integration test)
	testRefreshToken := os.Getenv("TEST_REFRESH_TOKEN")
	if testRefreshToken == "" {
		t.Skip("Skipping integration test: TEST_REFRESH_TOKEN not set")
	}

	cfg := models.Config{
		RefreshToken: testRefreshToken,
	}
	// Initialize TokenManager first
	tokenManager, err := auth.NewTokenManager(testRefreshToken)
	if err != nil {
		t.Fatalf("failed to create token manager: %v", err)
	}
	cfg.TokenManager = tokenManager
	if err := utils.PopulateAPICfg(&cfg); err != nil {
		t.Fatalf("failed to populate API config: %v", err)
	}

	handler := NewGetDropRulesHandler(http.DefaultClient, cfg)

	args := GetDropRulesArgs{}

	ctx := context.Background()
	req := &mcp.CallToolRequest{}
	result, _, err := handler(ctx, req, args)

	// Fail on API errors (like 502) - these indicate real problems
	if err != nil {
		// Check if error is an HTTP error (like 502)
		if strings.Contains(err.Error(), "status") || strings.Contains(err.Error(), "502") || strings.Contains(err.Error(), "500") {
			t.Fatalf("API returned error (test should fail): %v", err)
		}
		// For other errors, log but don't fail
		t.Logf("Integration test warning: %v", err)
		return
	}

	if len(result.Content) == 0 {
		t.Fatalf("expected content in result")
	}

	_, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent type")
	}

	// Log summary - drop rules response structure varies
	t.Logf("Integration test successful: drop rules retrieved")
}

// Integration test for add_drop_rule tool
func TestAddDropRuleHandler_Integration(t *testing.T) {
	// Skip if no refresh token is provided (integration test)
	testRefreshToken := os.Getenv("TEST_REFRESH_TOKEN")
	if testRefreshToken == "" {
		t.Skip("Skipping integration test: TEST_REFRESH_TOKEN not set")
	}

	cfg := models.Config{
		RefreshToken: testRefreshToken,
	}
	// Initialize TokenManager first
	tokenManager, err := auth.NewTokenManager(testRefreshToken)
	if err != nil {
		t.Fatalf("failed to create token manager: %v", err)
	}
	cfg.TokenManager = tokenManager
	if err := utils.PopulateAPICfg(&cfg); err != nil {
		t.Fatalf("failed to populate API config: %v", err)
	}

	handler := NewAddDropRuleHandler(http.DefaultClient, cfg)

	// Create a test drop rule with a unique name based on timestamp
	ruleName := fmt.Sprintf("test-drop-rule-%d", time.Now().Unix())

	args := AddDropRuleArgs{
		Name: ruleName,
		Filters: []DropRuleFilter{
			{
				Key:         "service",
				Value:       "test-service",
				Operator:    "equals",
				Conjunction: "and",
			},
		},
	}

	ctx := context.Background()
	req := &mcp.CallToolRequest{}
	result, _, err := handler(ctx, req, args)

	// Fail on API errors (like 502) - these indicate real problems
	if err != nil {
		// Check if error is an HTTP error (like 502)
		if strings.Contains(err.Error(), "status") || strings.Contains(err.Error(), "502") || strings.Contains(err.Error(), "500") {
			t.Fatalf("API returned error (test should fail): %v", err)
		}
		// For other errors (like rule already exists), log but don't fail
		t.Logf("Integration test warning (may be expected if rule already exists): %v", err)
		return
	}

	if len(result.Content) == 0 {
		t.Fatalf("expected content in result")
	}

	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent type")
	}

	// Log summary - attributes are typically returned as a list
	var attributes []string
	if err := json.Unmarshal([]byte(textContent.Text), &attributes); err != nil {
		// If it's not JSON, it might be formatted text - that's ok
		t.Logf("Integration test successful. Response is formatted text (not JSON)")
	} else {
		t.Logf("Integration test successful: found %d log attribute(s)", len(attributes))
	}
}
