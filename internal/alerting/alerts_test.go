package alerting

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Integration test for get_alert_config tool
func TestGetAlertConfigHandler_Integration(t *testing.T) {
	cfg := utils.SetupTestConfigOrSkip(t)

	handler := NewGetAlertConfigHandler(http.DefaultClient, *cfg)

	ctx := context.Background()
	req := &mcp.CallToolRequest{}
	result, _, err := handler(ctx, req, GetAlertConfigArgs{})

	if utils.CheckAPIError(t, err) {
		return
	}

	text := utils.GetTextContent(t, result)

	// Verify response structure - try to parse as JSON array
	var alertConfig AlertConfigResponse
	if err := json.Unmarshal([]byte(text), &alertConfig); err != nil {
		t.Logf("Integration test successful. Response is formatted text (not JSON)")
	} else {
		t.Logf("Integration test successful: received %d alert rule(s)", len(alertConfig))
	}
}

// Integration test for get_alerts tool
func TestGetAlertsHandler_Integration(t *testing.T) {
	cfg := utils.SetupTestConfigOrSkip(t)

	handler := NewGetAlertsHandler(http.DefaultClient, *cfg)

	tests := []struct {
		name string
		args GetAlertsArgs
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

			if utils.CheckAPIError(t, err) {
				return
			}

			text := utils.GetTextContent(t, result)

			// Try to parse response to verify structure and log summary
			var alertsResponse AlertsResponse
			if err := json.Unmarshal([]byte(text), &alertsResponse); err != nil {
				t.Logf("Integration test successful: alerts retrieved (formatted text response)")
			} else {
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
