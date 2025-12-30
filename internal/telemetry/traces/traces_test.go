package traces

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestGetTracesLimitParameter(t *testing.T) {
	tests := []struct {
		name          string
		limit         int
		wantErr       bool
		expectedLimit int
	}{
		{
			name:          "Default limit (no limit specified)",
			limit:         0, // 0 means not specified
			wantErr:       false,
			expectedLimit: 20, // Default limit
		},
		{
			name:          "Custom limit of 10",
			limit:         10,
			wantErr:       false,
			expectedLimit: 10,
		},
		{
			name:          "Custom limit of 50",
			limit:         50,
			wantErr:       false,
			expectedLimit: 50,
		},
		{
			name:          "Large limit of 150 (should be capped at 100)",
			limit:         150,
			wantErr:       false,
			expectedLimit: 100, // Maximum limit
		},
		{
			name:          "Custom limit of 1 (minimum)",
			limit:         1,
			wantErr:       false,
			expectedLimit: 1,
		},
		{
			name:          "Custom limit of 100 (maximum)",
			limit:         100,
			wantErr:       false,
			expectedLimit: 100,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a mock server to capture the request and verify the limit parameter
			receivedLimit := 0
			mockResponse := createMockTraceResponse(tt.expectedLimit)

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Verify request method and path
				if r.Method != "POST" {
					t.Errorf("Expected POST request, got %s", r.Method)
				}
				if !strings.Contains(r.URL.Path, "/cat/api/traces/v2/query_range/json") {
					t.Errorf("Expected traces API path, got %s", r.URL.Path)
				}

				// Extract limit from query parameters
				limitParam := r.URL.Query().Get("limit")
				if limitParam != "" {
					var err error
					receivedLimit, err = parseLimit(limitParam)
					if err != nil {
						t.Errorf("Failed to parse limit: %v", err)
					}
				}

				// Verify the limit matches expected
				if receivedLimit != tt.expectedLimit {
					t.Errorf("Expected limit %d in request, got %d", tt.expectedLimit, receivedLimit)
				}

				w.WriteHeader(http.StatusOK)
				io.WriteString(w, mockResponse)
			}))
			defer server.Close()

			cfg := models.Config{
				APIBaseURL: server.URL,
				Region:     "ap-south-1",
			}

			// Create a TokenManager with a fixed access token for testing
			// Set expiry far in the future to avoid token refresh during tests
			// The GetAccessToken method will return the token directly without refreshing
			tm := &auth.TokenManager{
				AccessToken: "mock-access-token-for-testing",
				ExpiresAt:   time.Now().Add(365 * 24 * time.Hour), // Valid for 1 year
			}
			cfg.TokenManager = tm

			handler := NewGetTracesHandler(server.Client(), cfg)

			// Build test arguments
			args := GetTracesArgs{
				TracejsonQuery: []interface{}{
					map[string]interface{}{
						"type": "filter",
						"query": map[string]interface{}{
							"$exists": []string{"ServiceName"},
						},
					},
				},
				LookbackMinutes: 60,
			}

			// Set limit if specified
			if tt.limit > 0 {
				args.Limit = tt.limit
			}

			ctx := context.Background()
			req := &mcp.CallToolRequest{}
			result, _, err := handler(ctx, req, args)

			if (err != nil) != tt.wantErr {
				t.Errorf("NewGetTracesHandler() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if err == nil {
				if len(result.Content) == 0 {
					t.Fatalf("expected content in result")
				}

				textContent, ok := result.Content[0].(*mcp.TextContent)
				if !ok {
					t.Fatalf("expected TextContent type")
				}

				var response map[string]interface{}
				if err := json.Unmarshal([]byte(textContent.Text), &response); err != nil {
					t.Fatalf("failed to unmarshal response: %v", err)
				}

				// Verify response structure
				if data, ok := response["data"].(map[string]interface{}); ok {
					if result, ok := data["result"].([]interface{}); ok {
						// The mock returns exactly the number of traces we expect
						if len(result) > tt.expectedLimit {
							t.Errorf("Expected at most %d traces in response, got %d", tt.expectedLimit, len(result))
						}
					}
				}
			}
		})
	}
}

func TestGetTracesHandler_ValidationErrors(t *testing.T) {
	cfg := models.Config{
		Region: "us-east-1",
	}

	handler := NewGetTracesHandler(http.DefaultClient, cfg)

	tests := []struct {
		name    string
		args    GetTracesArgs
		wantErr bool
		errMsg  string
	}{
		{
			name:    "Missing tracejson_query",
			args:    GetTracesArgs{},
			wantErr: true,
			errMsg:  "tracejson_query parameter is required",
		},
		{
			name: "Empty tracejson_query",
			args: GetTracesArgs{
				TracejsonQuery: []interface{}{},
			},
			wantErr: true,
			errMsg:  "tracejson_query parameter is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			req := &mcp.CallToolRequest{}
			_, _, err := handler(ctx, req, tt.args)

			if (err != nil) != tt.wantErr {
				t.Errorf("NewGetTracesHandler() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if err != nil && tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
				t.Errorf("NewGetTracesHandler() error = %v, want error containing %v", err, tt.errMsg)
			}
		})
	}
}

// Integration test - requires real API credentials
func TestGetTracesHandler_Integration(t *testing.T) {
	testRefreshToken := os.Getenv("TEST_REFRESH_TOKEN")
	if testRefreshToken == "" {
		t.Skip("Skipping integration test: TEST_REFRESH_TOKEN not set")
	}

	cfg := models.Config{
		RefreshToken:   testRefreshToken,
		DatasourceName: os.Getenv("TEST_DATASOURCE"), // Optional: use specific datasource for testing
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

	handler := NewGetTracesHandler(http.DefaultClient, cfg)

	tests := []struct {
		name  string
		limit int
	}{
		{
			name:  "Integration test with default limit",
			limit: 0, // Default
		},
		{
			name:  "Integration test with limit 10",
			limit: 10,
		},
		{
			name:  "Integration test with limit 50",
			limit: 50,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := GetTracesArgs{
				TracejsonQuery: []interface{}{
					map[string]interface{}{
						"type": "filter",
						"query": map[string]interface{}{
							"$exists": []string{"ServiceName"},
						},
					},
				},
				LookbackMinutes: 60,
			}

			if tt.limit > 0 {
				args.Limit = tt.limit
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
				// For other errors (like no traces), log but don't fail
				t.Logf("Integration test warning (may be expected if no traces exist): %v", err)
				return
			}

			if len(result.Content) == 0 {
				t.Fatalf("expected content in result")
			}

			textContent, ok := result.Content[0].(*mcp.TextContent)
			if !ok {
				t.Fatalf("expected TextContent type")
			}

			var response map[string]interface{}
			if err := json.Unmarshal([]byte(textContent.Text), &response); err != nil {
				t.Fatalf("failed to unmarshal response: %v", err)
			}

			// Verify response structure
			if response == nil {
				t.Fatalf("response is nil")
			}

			// Log summary instead of full response
			count := 0
			if data, ok := response["data"].(map[string]interface{}); ok {
				if result, ok := data["result"].([]interface{}); ok {
					count = len(result)
				}
			}
			t.Logf("Integration test successful for limit %d: received %d trace(s)", tt.limit, count)
		})
	}
}

// Helper function to parse limit from string
func parseLimit(limitStr string) (int, error) {
	var limit int
	_, err := fmt.Sscanf(limitStr, "%d", &limit)
	return limit, err
}

// Helper function to create a mock trace response with a specific number of traces
func createMockTraceResponse(numTraces int) string {
	traces := []map[string]interface{}{}
	for i := 0; i < numTraces && i < 10; i++ { // Cap at 10 for mock response
		traces = append(traces, map[string]interface{}{
			"TraceId":     fmt.Sprintf("trace-%d", i),
			"SpanId":      fmt.Sprintf("span-%d", i),
			"SpanKind":    "SPAN_KIND_SERVER",
			"SpanName":    "test-span",
			"ServiceName": "test-service",
			"Duration":    150000000,
			"Timestamp":   "2025-11-02T10:00:00Z",
			"StatusCode":  "STATUS_CODE_OK",
		})
	}

	response := map[string]interface{}{
		"data": map[string]interface{}{
			"result": traces,
		},
	}

	jsonBytes, _ := json.Marshal(response)
	return string(jsonBytes)
}

// Integration test for get_trace_attributes tool
func TestGetTraceAttributesHandler_Integration(t *testing.T) {
	cfg := utils.SetupTestConfigOrSkip(t)

	handler := NewGetTraceAttributesHandler(http.DefaultClient, *cfg)

	args := GetTraceAttributesArgs{
		LookbackMinutes: 15,
	}

	ctx := context.Background()
	req := &mcp.CallToolRequest{}
	result, _, err := handler(ctx, req, args)

	if utils.CheckAPIError(t, err) {
		return
	}

	text := utils.GetTextContent(t, result)

	var attributes []string
	if err := json.Unmarshal([]byte(text), &attributes); err != nil {
		t.Logf("Integration test successful. Response is formatted text (not JSON)")
	} else {
		t.Logf("Integration test successful: found %d trace attribute(s)", len(attributes))
	}
}

// Integration test for get_exceptions tool
func TestGetExceptionsHandler_Integration(t *testing.T) {
	cfg := utils.SetupTestConfigOrSkip(t)

	handler := NewGetExceptionsHandler(http.DefaultClient, *cfg)

	args := GetExceptionsArgs{
		LookbackMinutes: 60,
		Limit:           20,
	}

	ctx := context.Background()
	req := &mcp.CallToolRequest{}
	result, _, err := handler(ctx, req, args)

	if utils.CheckAPIError(t, err) {
		return
	}

	text := utils.GetTextContent(t, result)

	var response map[string]interface{}
	if err := json.Unmarshal([]byte(text), &response); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	count := 0
	if data, ok := response["data"].(map[string]interface{}); ok {
		if result, ok := data["result"].([]interface{}); ok {
			count = len(result)
		}
	}
	t.Logf("Integration test successful: received %d exception(s)", count)
}
