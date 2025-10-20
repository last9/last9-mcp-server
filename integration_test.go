package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	last9mcp "github.com/last9/mcp-go-sdk/mcp"
)

// MockLast9Server creates a mock Last9 API server for testing
type MockLast9Server struct {
	*httptest.Server
	RequestCount   int
	TokenRefreshed bool
	ExpiredToken   string
	ValidToken     string
}

// NewMockLast9Server creates a new mock server that simulates Last9 API
func NewMockLast9Server() *MockLast9Server {
	mock := &MockLast9Server{
		ExpiredToken: "expired_token_12345",
		ValidToken:   "valid_token_67890",
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mock.RequestCount++

		// Handle token refresh endpoint
		if strings.Contains(r.URL.Path, "/oauth/access_token") {
			mock.TokenRefreshed = true
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{
				"access_token": mock.ValidToken,
			})
			return
		}

		// Handle datasources endpoint
		if strings.Contains(r.URL.Path, "/datasources") {
			authHeader := r.Header.Get("X-LAST9-API-TOKEN")

			// Simulate expired token on first request
			if strings.Contains(authHeader, mock.ExpiredToken) {
				w.WriteHeader(http.StatusForbidden)
				w.Write([]byte(`{"error":"token expired"}`))
				return
			}

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]map[string]interface{}{
				{
					"is_default": true,
					"url":        "http://prometheus.example.com",
					"properties": map[string]string{
						"username": "test_user",
						"password": "test_pass",
					},
				},
			})
			return
		}

		// Handle Prometheus queries
		if strings.Contains(r.URL.Path, "/prom_query") ||
		   strings.Contains(r.URL.Path, "/prom_query_instant") ||
		   strings.Contains(r.URL.Path, "/prom_label_values") ||
		   strings.Contains(r.URL.Path, "/apm/labels") {
			authHeader := r.Header.Get("X-LAST9-API-TOKEN")

			// Simulate expired token
			if strings.Contains(authHeader, mock.ExpiredToken) {
				w.WriteHeader(http.StatusUnauthorized)
				w.Write([]byte(`{"error":"unauthorized"}`))
				return
			}

			// Return mock Prometheus response
			w.Header().Set("Content-Type", "application/json")
			mockResponse := map[string]interface{}{
				"data": map[string]interface{}{
					"result": []map[string]interface{}{
						{
							"metric": map[string]string{
								"service_name": "test-service",
								"env":          "production",
							},
							"value": []interface{}{time.Now().Unix(), "100"},
						},
					},
				},
			}
			json.NewEncoder(w).Encode(mockResponse)
			return
		}

		// Handle logs queries
		if strings.Contains(r.URL.Path, "/logs/api/v2/query_range") {
			authHeader := r.Header.Get("X-LAST9-API-TOKEN")

			if strings.Contains(authHeader, mock.ExpiredToken) {
				w.WriteHeader(http.StatusForbidden)
				w.Write([]byte(`{"error":"forbidden"}`))
				return
			}

			w.Header().Set("Content-Type", "application/json")
			mockLogs := map[string]interface{}{
				"result": []map[string]interface{}{
					{
						"Timestamp":    time.Now().UnixNano(),
						"Body":         "test log message",
						"SeverityText": "INFO",
						"ServiceName":  "test-service",
					},
				},
			}
			json.NewEncoder(w).Encode(mockLogs)
			return
		}

		// Default response
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})

	mock.Server = httptest.NewServer(handler)
	return mock
}

// createTestConfig creates a test configuration with mock refresh token
func createTestConfig(mockServer *MockLast9Server, expiredToken bool) models.Config {
	// Create a valid JWT-like refresh token with proper base64 encoding
	// The aud field must match the mock server URL for token refresh to work
	// Header: {"alg":"HS256","typ":"JWT"}
	// Payload must be manually constructed with the mock server URL

	// Encode payload: {"organization_slug":"test-org","aud":["<mockServer.URL>"]}
	payload := map[string]interface{}{
		"organization_slug": "test-org",
		"aud":              []string{mockServer.URL},
	}
	payloadBytes, _ := json.Marshal(payload)
	payloadB64 := base64.RawURLEncoding.EncodeToString(payloadBytes)

	refreshToken := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9." + payloadB64 + ".fake_signature"

	token := mockServer.ValidToken
	if expiredToken {
		token = mockServer.ExpiredToken
	}

	// Extract base URL without scheme for GetDefaultRegion
	baseURL := strings.TrimPrefix(mockServer.URL, "http://")
	baseURL = strings.TrimPrefix(baseURL, "https://")

	return models.Config{
		BaseURL:      baseURL,
		AuthToken:    "Basic test_token",
		RefreshToken: refreshToken,
		AccessToken:  token,
		APIBaseURL:   mockServer.URL + "/api/v4/organizations/test-org",
		OrgSlug:      "test-org",
		PrometheusReadURL: "http://prometheus.example.com",
		PrometheusUsername: "test_user",
		PrometheusPassword: "test_pass",
	}
}

// TestMCPServerIntegration tests the entire MCP server with mock data
func TestMCPServerIntegration(t *testing.T) {
	mockServer := NewMockLast9Server()
	defer mockServer.Close()

	cfg := createTestConfig(mockServer, false)

	// Create MCP server
	server, err := last9mcp.NewServer("last9-mcp-test", "test-version")
	if err != nil {
		t.Fatalf("Failed to create MCP server: %v", err)
	}

	// Register all tools
	if err := registerAllTools(server, cfg); err != nil {
		t.Fatalf("Failed to register tools: %v", err)
	}

	// Just verify server was created successfully
	if server == nil {
		t.Fatal("Server should not be nil")
	}

	t.Log("MCP server created and tools registered successfully")
}

// TestTokenRefreshOnExpiry tests that expired tokens are automatically refreshed
func TestTokenRefreshOnExpiry(t *testing.T) {
	mockServer := NewMockLast9Server()
	defer mockServer.Close()

	// Start with expired token
	cfg := createTestConfig(mockServer, true)

	client := &http.Client{Timeout: 10 * time.Second}

	t.Run("automatic token refresh on 401", func(t *testing.T) {
		// Make a Prometheus query that will fail with 401 and trigger refresh
		query := "sum(rate(http_requests_total[5m]))"
		endTime := time.Now().Unix()

		mockServer.TokenRefreshed = false
		resp, err := utils.MakePromInstantAPIQuery(context.Background(), client, query, endTime, &cfg)
		if err != nil {
			t.Fatalf("Query failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("Expected 200 OK after token refresh, got %d: %s", resp.StatusCode, string(body))
		}

		if !mockServer.TokenRefreshed {
			t.Error("Token was not refreshed")
		}

		if cfg.AccessToken != mockServer.ValidToken {
			t.Errorf("Access token not updated, got %s, want %s", cfg.AccessToken, mockServer.ValidToken)
		}
	})

	t.Run("automatic token refresh on 403", func(t *testing.T) {
		// Reset the token to expired
		cfg.AccessToken = mockServer.ExpiredToken
		mockServer.TokenRefreshed = false

		// Make a range query
		query := "sum(rate(http_requests_total[5m]))"
		startTime := time.Now().Add(-1 * time.Hour).Unix()
		endTime := time.Now().Unix()

		resp, err := utils.MakePromRangeAPIQuery(context.Background(), client, query, startTime, endTime, &cfg)
		if err != nil {
			t.Fatalf("Range query failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("Expected 200 OK after token refresh, got %d: %s", resp.StatusCode, string(body))
		}

		if !mockServer.TokenRefreshed {
			t.Error("Token was not refreshed on 403")
		}
	})
}

// TestMCPToolsWithMockData tests various MCP tools with mock data
func TestMCPToolsWithMockData(t *testing.T) {
	mockServer := NewMockLast9Server()
	defer mockServer.Close()

	cfg := createTestConfig(mockServer, false)

	client := &http.Client{Timeout: 10 * time.Second}

	tests := []struct {
		name     string
		testFunc func(t *testing.T)
	}{
		{
			name: "prometheus instant query",
			testFunc: func(t *testing.T) {
				query := "up"
				endTime := time.Now().Unix()
				resp, err := utils.MakePromInstantAPIQuery(context.Background(), client, query, endTime, &cfg)
				if err != nil {
					t.Fatalf("Instant query failed: %v", err)
				}
				defer resp.Body.Close()

				if resp.StatusCode != http.StatusOK {
					t.Errorf("Expected 200, got %d", resp.StatusCode)
				}
			},
		},
		{
			name: "prometheus range query",
			testFunc: func(t *testing.T) {
				query := "up"
				startTime := time.Now().Add(-1 * time.Hour).Unix()
				endTime := time.Now().Unix()
				resp, err := utils.MakePromRangeAPIQuery(context.Background(), client, query, startTime, endTime, &cfg)
				if err != nil {
					t.Fatalf("Range query failed: %v", err)
				}
				defer resp.Body.Close()

				if resp.StatusCode != http.StatusOK {
					t.Errorf("Expected 200, got %d", resp.StatusCode)
				}
			},
		},
		{
			name: "prometheus label values query",
			testFunc: func(t *testing.T) {
				startTime := time.Now().Add(-1 * time.Hour).Unix()
				endTime := time.Now().Unix()
				resp, err := utils.MakePromLabelValuesAPIQuery(context.Background(), client, "service_name", "up", startTime, endTime, &cfg)
				if err != nil {
					t.Fatalf("Label values query failed: %v", err)
				}
				defer resp.Body.Close()

				if resp.StatusCode != http.StatusOK {
					t.Errorf("Expected 200, got %d", resp.StatusCode)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, tt.testFunc)
	}
}

// TestErrorHandling tests error scenarios
func TestErrorHandling(t *testing.T) {
	mockServer := NewMockLast9Server()
	defer mockServer.Close()

	_ = createTestConfig(mockServer, false)

	t.Run("invalid time range", func(t *testing.T) {
		params := map[string]interface{}{
			"start_time_iso": "2024-01-01 10:00:00",
			"end_time_iso":   "2024-01-01 09:00:00", // end before start
		}

		_, _, err := utils.GetTimeRange(params, 60)
		if err == nil {
			t.Error("Expected error for invalid time range")
		}
		if !strings.Contains(err.Error(), "start_time cannot be after end_time") {
			t.Errorf("Expected time range error, got: %v", err)
		}
	})

	t.Run("invalid lookback minutes", func(t *testing.T) {
		params := map[string]interface{}{
			"lookback_minutes": float64(2000), // exceeds max
		}

		_, _, err := utils.GetTimeRange(params, 60)
		if err == nil {
			t.Error("Expected error for invalid lookback minutes")
		}
		if !strings.Contains(err.Error(), "cannot exceed 1440") {
			t.Errorf("Expected lookback error, got: %v", err)
		}
	})
}

// TestConcurrentTokenRefresh tests that concurrent requests handle token refresh correctly
func TestConcurrentTokenRefresh(t *testing.T) {
	mockServer := NewMockLast9Server()
	defer mockServer.Close()

	_ = createTestConfig(mockServer, true)
	client := &http.Client{Timeout: 10 * time.Second}

	// Create a fresh config for concurrent testing
	cfg := createTestConfig(mockServer, true)

	// Make multiple concurrent requests that should trigger token refresh
	numRequests := 10
	errChan := make(chan error, numRequests)

	for i := 0; i < numRequests; i++ {
		go func() {
			query := "up"
			endTime := time.Now().Unix()
			resp, err := utils.MakePromInstantAPIQuery(context.Background(), client, query, endTime, &cfg)
			if err != nil {
				errChan <- err
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				errChan <- err
				return
			}
			errChan <- nil
		}()
	}

	// Collect results
	for i := 0; i < numRequests; i++ {
		err := <-errChan
		if err != nil {
			t.Errorf("Request %d failed: %v", i, err)
		}
	}

	if !mockServer.TokenRefreshed {
		t.Error("Token was not refreshed during concurrent requests")
	}

	if cfg.AccessToken != mockServer.ValidToken {
		t.Error("Config was not updated with new token")
	}
}
