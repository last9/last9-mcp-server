package alerting

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
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

	tests := []struct {
		name string
		args GetAlertConfigArgs
	}{
		{
			name: "unfiltered",
			args: GetAlertConfigArgs{},
		},
		{
			name: "filtered by rule type",
			args: GetAlertConfigArgs{
				RuleType: "static",
			},
		},
	}

	// Separate sub-test: fetch a real rule ID then verify rule_id filter returns exactly that rule.
	t.Run("filtered by rule_id", func(t *testing.T) {
		ctx := context.Background()

		// Step 1: get all rules to find a real ID.
		result, _, err := handler(ctx, &mcp.CallToolRequest{}, GetAlertConfigArgs{})
		if utils.CheckAPIError(t, err) {
			return
		}
		text := utils.GetTextContent(t, result)

		// Extract the first ID from the formatted response ("  ID: <uuid>").
		var firstID string
		for _, line := range strings.Split(text, "\n") {
			if id, ok := strings.CutPrefix(strings.TrimSpace(line), "ID: "); ok {
				firstID = id
				break
			}
		}
		if firstID == "" {
			t.Skip("no alert rules returned by API — skipping rule_id filter test")
		}

		// Step 2: query by that specific rule ID.
		result, _, err = handler(ctx, &mcp.CallToolRequest{}, GetAlertConfigArgs{RuleID: firstID})
		if utils.CheckAPIError(t, err) {
			return
		}
		text = utils.GetTextContent(t, result)

		if !strings.Contains(text, "Found 1 alert rules:") {
			t.Fatalf("expected exactly 1 rule for rule_id=%q, got:\n%s", firstID, text)
		}
		if !strings.Contains(text, "ID: "+firstID) {
			t.Fatalf("response does not contain expected rule ID %q:\n%s", firstID, text)
		}
		t.Logf("Integration test successful: rule_id=%s returned exactly 1 rule", firstID)
	})

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			req := &mcp.CallToolRequest{}
			result, _, err := handler(ctx, req, tt.args)

			if utils.CheckAPIError(t, err) {
				return
			}

			text := utils.GetTextContent(t, result)
			if text == "" {
				t.Fatal("expected non-empty response")
			}

			t.Logf("Integration test successful for %s", tt.name)
		})
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

func TestGetAlertsHandler_WindowValidation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		window, _ := strconv.ParseInt(r.URL.Query().Get("window"), 10, 64)
		resp := AlertsResponse{
			Timestamp:  time.Now().Unix(),
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
		name        string
		args        GetAlertsArgs
		wantErr     bool
		errContains string
	}{
		{
			name:    "window=1 (API minimum) is accepted",
			args:    GetAlertsArgs{Window: 1},
			wantErr: false,
		},
		{
			name:    "window=900 (default) is accepted",
			args:    GetAlertsArgs{Window: 900},
			wantErr: false,
		},
		{
			name:    "window=3600 (API maximum) is accepted",
			args:    GetAlertsArgs{Window: 3600},
			wantErr: false,
		},
		{
			name:        "window=0 uses default (accepted)",
			args:        GetAlertsArgs{Window: 0},
			wantErr:     false,
		},
		{
			name:        "window=3601 exceeds API maximum",
			args:        GetAlertsArgs{Window: 3601},
			wantErr:     true,
			errContains: "window must be between 1 and 3600",
		},
		{
			name:        "window=5400 (90 min) exceeds API maximum",
			args:        GetAlertsArgs{Window: 5400},
			wantErr:     true,
			errContains: "window must be between 1 and 3600",
		},
		{
			name:        "window=86400 far exceeds API maximum",
			args:        GetAlertsArgs{Window: 86400},
			wantErr:     true,
			errContains: "window must be between 1 and 3600",
		},
		{
			name:        "lookback_minutes=61 exceeds max",
			args:        GetAlertsArgs{LookbackMinutes: 61},
			wantErr:     true,
			errContains: "lookback_minutes must be between 1 and 60",
		},
		{
			name:    "lookback_minutes=60 (API maximum) is accepted",
			args:    GetAlertsArgs{LookbackMinutes: 60},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, tt.args)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error but got nil")
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Fatalf("error %q does not contain %q", err.Error(), tt.errContains)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
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
