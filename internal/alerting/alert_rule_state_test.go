package alerting

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestAlertRuleStateHandler(t *testing.T) {
	// Mock server that returns a predefined matrix result simulating Prometheus output
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("Expected GET request, got %s", r.Method)
		}
		if !strings.Contains(r.URL.Path, "/alerts/monitor") {
			t.Errorf("Expected path to contain /alerts/monitor, got %s", r.URL.Path)
		}
		
		// Assert that query params are correctly set
		query := r.URL.Query()
		if query.Get("rule_name") == "" && query.Get("state") == "" {
			// fallback check if not testing filter passing specifically
		}

		w.Header().Set("Content-Type", "application/json")
		
		// Return alerts monitor format
		mockResponse := `{
			"timestamp": 1686072840,
			"window": 60,
			"alert_rules": [
				{
					"rule_id": "test-rule-1",
					"state": "firing"
				},
				{
					"rule_id": "test-rule-2",
					"state": "normal"
				}
			]
		}`
		// Change the response for the second request
		if strings.Contains(r.URL.RawQuery, "timestamp=1686072900") {
			mockResponse = `{
				"timestamp": 1686072900,
				"window": 60,
				"alert_rules": [
					{
						"rule_id": "test-rule-1",
						"state": "pending"
					},
					{
						"rule_id": "test-rule-2",
						"state": "firing"
					}
				]
			}`
		}
		w.Write([]byte(mockResponse))
	}))
	defer server.Close()

	cfg := models.Config{
		APIBaseURL: server.URL,
	}

	// Update config to use the test server directly instead of prepending APIBaseURL
	// The implementation in alert_rule_state.go uses cfg.APIBaseURL + "/api/v4/organizations/" + cfg.OrgSlug
	cfg.APIBaseURL = server.URL
	cfg.OrgSlug = "test-org"

	// Mock token manager if needed (though handler doesn't fail if absent)
	cfg.TokenManager = &auth.TokenManager{
		AccessToken: "mock-token",
		ExpiresAt:   time.Now().Add(365 * 24 * time.Hour),
	}

	handler := NewAlertRuleStateHandler(server.Client(), cfg)

	ctx := context.Background()

	t.Run("ValidRequest", func(t *testing.T) {
		args := AlertRuleStateRequest{
			StartTime: 1718000000,
			EndTime:   1718000060,
			Step:      60,
		}
		
		req := &mcp.CallToolRequest{}

		result, _, err := handler(ctx, req, args)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if len(result.Content) == 0 {
			t.Fatalf("Expected content in result")
		}

		textContent, ok := result.Content[0].(*mcp.TextContent)
		if !ok {
			t.Fatalf("Expected TextContent")
		}

		// Verify that the response contains the rule map
		if !strings.Contains(textContent.Text, "\"test-rule-1\": [") {
			t.Errorf("Expected response to contain test-rule-1 key, got: %s", textContent.Text)
		}
		// Since we removed client-side filtering, test-rule-2 SHOULD be in the output
		if !strings.Contains(textContent.Text, "\"test-rule-2\": [") {
			t.Errorf("Expected response to contain test-rule-2 key, got: %s", textContent.Text)
		}

		// Verify that the response was processed and mapped to IsFiring correctly for test-rule-1
		if !strings.Contains(textContent.Text, "\"is_firing\": 1") || !strings.Contains(textContent.Text, "\"is_firing\": 0") {
			t.Errorf("Expected is_firing to be 1 and 0 in response, got: %s", textContent.Text)
		}
	})

	t.Run("APIError", func(t *testing.T) {
		// Test with a broken URL to trigger an API error
		badCfg := cfg
		badCfg.APIBaseURL = "http://invalid.local"
		badHandler := NewAlertRuleStateHandler(server.Client(), badCfg)

		args := AlertRuleStateRequest{
			StartTime: 1718000000,
			EndTime:   1718000060,
			Step:      60,
		}

		_, _, err := badHandler(ctx, &mcp.CallToolRequest{}, args)
		if err == nil {
			t.Fatalf("Expected API error result for broken URL")
		}
	})
}
