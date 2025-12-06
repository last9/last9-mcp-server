package change_events

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Integration test for get_change_events tool
func TestGetChangeEventsHandler_Integration(t *testing.T) {
	// Skip if no refresh token is provided (integration test)
	cfg, err := utils.SetupTestConfig()
	if err != nil {
		if _, ok := err.(*utils.TestConfigError); ok {
			t.Skipf("Skipping test: %v", err)
		}
		t.Fatalf("failed to setup test config: %v", err)
	}

	handler := NewGetChangeEventsHandler(http.DefaultClient, *cfg)

	tests := []struct {
		name string
		args GetChangeEventsArgs
	}{
		{
			name: "Get change events with default parameters",
			args: GetChangeEventsArgs{
				LookbackMinutes: 60,
			},
		},
		{
			name: "Get change events with service filter",
			args: GetChangeEventsArgs{
				LookbackMinutes: 30,
				Service:         "test-service",
			},
		},
		{
			name: "Get change events with environment filter",
			args: GetChangeEventsArgs{
				LookbackMinutes: 60,
				Environment:     "prod",
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

			// Verify response structure and log summary
			var response map[string]interface{}
			if err := json.Unmarshal([]byte(textContent.Text), &response); err != nil {
				t.Fatalf("failed to unmarshal response: %v", err)
			}

			// Log summary instead of full response
			count := 0
			if changeEvents, ok := response["change_events"].([]interface{}); ok {
				count = len(changeEvents)
			}
			availableEventNames := 0
			if eventNames, ok := response["available_event_names"].([]interface{}); ok {
				availableEventNames = len(eventNames)
			}
			t.Logf("Integration test successful: %d change event(s), %d available event name(s)",
				count, availableEventNames)
		})
	}
}
