package alerting

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/models"
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

func TestGetAlertsHandler_TimeParameterPrecedence(t *testing.T) {
	type capturedRequest struct {
		timestamp int64
		window    int64
	}

	var captured []capturedRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ts, _ := strconv.ParseInt(r.URL.Query().Get("timestamp"), 10, 64)
		window, _ := strconv.ParseInt(r.URL.Query().Get("window"), 10, 64)
		captured = append(captured, capturedRequest{
			timestamp: ts,
			window:    window,
		})

		resp := AlertsResponse{
			Timestamp:  ts,
			Window:     window,
			AlertRules: []AlertRuleData{},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cfg := models.Config{
		APIBaseURL: server.URL,
	}
	cfg.TokenManager = &auth.TokenManager{
		AccessToken: "mock-token",
		ExpiresAt:   time.Now().Add(365 * 24 * time.Hour),
	}

	handler := NewGetAlertsHandler(server.Client(), cfg)

	tests := []struct {
		name            string
		args            GetAlertsArgs
		expectedWindow  int64
		expectedTSExact int64
	}{
		{
			name: "lookback_minutes maps to window",
			args: GetAlertsArgs{
				LookbackMinutes: 30,
			},
			expectedWindow: 1800,
		},
		{
			name: "window takes precedence over lookback",
			args: GetAlertsArgs{
				Window:          1200,
				LookbackMinutes: 30,
			},
			expectedWindow: 1200,
		},
		{
			name: "explicit timestamp and window take precedence",
			args: GetAlertsArgs{
				Timestamp:       1700000000,
				Window:          1500,
				LookbackMinutes: 30,
			},
			expectedWindow:  1500,
			expectedTSExact: 1700000000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := len(captured)
			_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, tt.args)
			if err != nil {
				t.Fatalf("handler returned error: %v", err)
			}

			if len(captured) != before+1 {
				t.Fatalf("expected one captured request, got %d", len(captured)-before)
			}

			got := captured[len(captured)-1]
			if got.window != tt.expectedWindow {
				t.Fatalf("window = %d, want %d", got.window, tt.expectedWindow)
			}

			if tt.expectedTSExact != 0 {
				if got.timestamp != tt.expectedTSExact {
					t.Fatalf("timestamp = %d, want %d", got.timestamp, tt.expectedTSExact)
				}
				return
			}

			now := time.Now().Unix()
			if got.timestamp < now-10 || got.timestamp > now+10 {
				t.Fatalf("timestamp = %d, expected near now (%d)", got.timestamp, now)
			}
		})
	}
}
