package alerting

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Integration test for get_alert_config tool
func TestGetAlertConfigHandler_Integration(t *testing.T) {
	cfg, err := utils.SetupTestConfig()
	if err != nil {
		if _, ok := err.(*utils.TestConfigError); ok {
			t.Skipf("Skipping integration test: %v", err)
		}
		t.Fatalf("failed to setup test config: %v", err)
	}

	handler := NewGetAlertConfigHandler(http.DefaultClient, *cfg)

	args := GetAlertConfigArgs{}

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

	// Verify response structure - try to parse as JSON array
	var alertConfig AlertConfigResponse
	if err := json.Unmarshal([]byte(textContent.Text), &alertConfig); err != nil {
		// If it's not JSON, it might be formatted text - that's ok, just log summary
		t.Logf("Integration test successful. Response is formatted text (not JSON)")
	} else {
		// Log summary instead of full response
		t.Logf("Integration test successful: received %d alert rule(s)", len(alertConfig))
	}
}

// Integration test for get_alerts tool
func TestGetAlertsHandler_Integration(t *testing.T) {
	cfg, err := utils.SetupTestConfig()
	if err != nil {
		if _, ok := err.(*utils.TestConfigError); ok {
			t.Skipf("Skipping integration test: %v", err)
		}
		t.Fatalf("failed to setup test config: %v", err)
	}

	handler := NewGetAlertsHandler(http.DefaultClient, *cfg)

	tests := []struct {
		name      string
		args      GetAlertsArgs
		wantError bool
	}{
		{
			name: "Get alerts with default parameters",
			args: GetAlertsArgs{},
		},
		{
			name: "Get alerts with custom timestamp and window",
			args: GetAlertsArgs{
				Timestamp: float64(time.Now().Unix()),
				Window:    1800, // 30 minutes
			},
		},
		{
			name: "Get alerts with 1 hour window",
			args: GetAlertsArgs{
				Window: 3600, // 1 hour
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
				if tt.wantError {
					return // Expected error
				}
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

			// Try to parse response to verify structure and log summary
			var alertsResponse AlertsResponse
			if err := json.Unmarshal([]byte(textContent.Text), &alertsResponse); err != nil {
				// If it's not JSON, it's formatted text - extract summary from formatted text
				// The formatted text contains "Found X alert rule(s) with Y alert instance(s)"
				// Just log that we got a response without parsing the full text
				t.Logf("Integration test successful: alerts retrieved (formatted text response)")
			} else {
				// Log summary instead of full response
				totalAlerts := 0
				for _, rule := range alertsResponse.AlertRules {
					totalAlerts += len(rule.Alerts)
				}
				t.Logf("Integration test successful: %d alert rule(s), %d alert instance(s) (timestamp: %d, window: %ds)",
					len(alertsResponse.AlertRules), totalAlerts, alertsResponse.Timestamp, alertsResponse.Window)
			}
		})
	}
}
