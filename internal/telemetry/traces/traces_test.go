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
			expectedLimit: models.DefaultMaxGetTracesEntries,
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
			name:          "Large limit of 150 is forwarded",
			limit:         150,
			wantErr:       false,
			expectedLimit: 150,
		},
		{
			name:          "Custom limit of 1 (minimum)",
			limit:         1,
			wantErr:       false,
			expectedLimit: 1,
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

			// Build test arguments with a ≤5-minute window so chunking produces a single chunk
			now := time.Now().UTC()
			args := GetTracesArgs{
				TracejsonQuery: []map[string]interface{}{
					{
						"type": "filter",
						"query": map[string]interface{}{
							"$exists": []string{"ServiceName"},
						},
					},
				},
				StartTimeISO: now.Add(-5 * time.Minute).Format(time.RFC3339),
				EndTimeISO:   now.Format(time.RFC3339),
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

				// Verify compact response structure
				count, ok := response["count"].(float64)
				if !ok {
					t.Fatalf("expected 'count' field in compact response")
				}
				if int(count) > tt.expectedLimit {
					t.Errorf("Expected at most %d traces in response, got %d", tt.expectedLimit, int(count))
				}
				traces, ok := response["traces"].([]interface{})
				if !ok {
					t.Fatalf("expected 'traces' array in compact response")
				}
				if len(traces) > tt.expectedLimit {
					t.Errorf("Expected at most %d traces, got %d", tt.expectedLimit, len(traces))
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
				TracejsonQuery: []map[string]interface{}{},
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

func TestExtractExactTraceIDLookup(t *testing.T) {
	tests := []struct {
		name     string
		pipeline []map[string]interface{}
		wantID   string
		wantOK   bool
	}{
		{
			name: "direct exact trace id equality",
			pipeline: []map[string]interface{}{
				{
					"type": "filter",
					"query": map[string]interface{}{
						"$eq": []interface{}{"TraceId", "trace-123"},
					},
				},
			},
			wantID: "trace-123",
			wantOK: true,
		},
		{
			name: "nested exact trace id equality",
			pipeline: []map[string]interface{}{
				{
					"type": "filter",
					"query": map[string]interface{}{
						"$and": []interface{}{
							map[string]interface{}{
								"$eq": []interface{}{"ServiceName", "api"},
							},
							map[string]interface{}{
								"$eq": []interface{}{"TraceId", "trace-123"},
							},
						},
					},
				},
			},
			wantID: "trace-123",
			wantOK: true,
		},
		{
			name: "or exact trace id equality",
			pipeline: []map[string]interface{}{
				{
					"type": "filter",
					"query": map[string]interface{}{
						"$or": []interface{}{
							"skip-non-map-entry",
							map[string]interface{}{
								"$eq": []interface{}{"TraceId", "trace-123"},
							},
						},
					},
				},
			},
			wantID: "trace-123",
			wantOK: true,
		},
		{
			name: "trace id contains is not exact lookup",
			pipeline: []map[string]interface{}{
				{
					"type": "filter",
					"query": map[string]interface{}{
						"$contains": []interface{}{"TraceId", "trace-123"},
					},
				},
			},
			wantOK: false,
		},
		{
			name: "non filter pipeline does not match",
			pipeline: []map[string]interface{}{
				{
					"type": "aggregate",
				},
			},
			wantOK: false,
		},
		{
			name: "multiple pipeline steps do not match",
			pipeline: []map[string]interface{}{
				{
					"type": "filter",
					"query": map[string]interface{}{
						"$eq": []interface{}{"TraceId", "trace-123"},
					},
				},
				{
					"type": "aggregate",
				},
			},
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotID, gotOK := extractExactTraceIDLookup(tt.pipeline)
			if gotOK != tt.wantOK {
				t.Fatalf("extractExactTraceIDLookup() ok = %v, want %v", gotOK, tt.wantOK)
			}
			if gotID != tt.wantID {
				t.Fatalf("extractExactTraceIDLookup() id = %q, want %q", gotID, tt.wantID)
			}
		})
	}
}

func TestCompactTraceResponse_SpanResults(t *testing.T) {
	raw := map[string]interface{}{
		"data": map[string]interface{}{
			"result": []interface{}{
				map[string]interface{}{
					"TraceId":     "abc123",
					"SpanId":      "span-1",
					"SpanKind":    "SPAN_KIND_SERVER",
					"SpanName":    "GET /api",
					"ServiceName": "api-service",
					"Duration":    float64(150000),
					"Timestamp":   "2025-11-02T10:00:00Z",
					"StatusCode":  "STATUS_CODE_OK",
					// Verbose fields that should be stripped
					"ResourceAttributes": map[string]interface{}{
						"service.namespace": "prod",
						"k8s.pod.name":      "api-service-abc-123",
						"host.name":         "ip-10-0-1-5",
					},
					"SpanAttributes": map[string]interface{}{
						"http.method":      "GET",
						"http.url":         "https://api.example.com/users",
						"http.status_code": float64(200),
					},
					"Events": []interface{}{},
					"Links":  []interface{}{},
				},
			},
		},
	}

	compact := compactTraceResponse(raw)

	count, ok := compact["count"].(int)
	if !ok || count != 1 {
		t.Fatalf("expected count=1, got %v", compact["count"])
	}

	traces, ok := compact["traces"].([]map[string]interface{})
	if !ok || len(traces) != 1 {
		t.Fatalf("expected 1 trace, got %v", compact["traces"])
	}

	tr := traces[0]
	if tr["trace_id"] != "abc123" {
		t.Errorf("expected trace_id=abc123, got %v", tr["trace_id"])
	}
	if tr["service_name"] != "api-service" {
		t.Errorf("expected service_name=api-service, got %v", tr["service_name"])
	}

	// Verify verbose fields are NOT present
	if _, exists := tr["ResourceAttributes"]; exists {
		t.Error("ResourceAttributes should be stripped from compact response")
	}
	if _, exists := tr["SpanAttributes"]; exists {
		t.Error("SpanAttributes should be stripped from compact response")
	}

	// Verify size reduction: compact JSON should be much smaller than raw
	rawJSON, _ := json.Marshal(raw)
	compactJSON, _ := json.Marshal(compact)
	if len(compactJSON) >= len(rawJSON) {
		t.Errorf("compact response (%d bytes) should be smaller than raw (%d bytes)", len(compactJSON), len(rawJSON))
	}
}

func TestCompactTraceResponse_EmptyResults(t *testing.T) {
	raw := map[string]interface{}{
		"data": map[string]interface{}{
			"result": []interface{}{},
		},
	}

	compact := compactTraceResponse(raw)

	count, ok := compact["count"].(int)
	if !ok || count != 0 {
		t.Fatalf("expected count=0, got %v", compact["count"])
	}

	traces, ok := compact["traces"].([]interface{})
	if !ok || len(traces) != 0 {
		t.Fatalf("expected empty traces array, got %v", compact["traces"])
	}
}

func TestCompactTraceResponse_AggregationResults(t *testing.T) {
	// Aggregation results don't have TraceId — should be returned as-is
	raw := map[string]interface{}{
		"data": map[string]interface{}{
			"result": []interface{}{
				map[string]interface{}{
					"ServiceName": "api-service",
					"count":       float64(42),
				},
				map[string]interface{}{
					"ServiceName": "web-service",
					"count":       float64(18),
				},
			},
		},
	}

	compact := compactTraceResponse(raw)

	count, ok := compact["count"].(int)
	if !ok || count != 2 {
		t.Fatalf("expected count=2, got %v", compact["count"])
	}

	// Aggregation results should be under "data", not "traces"
	data, ok := compact["data"].([]interface{})
	if !ok {
		t.Fatalf("expected 'data' field for aggregation results, got %v", compact)
	}
	if len(data) != 2 {
		t.Errorf("expected 2 data items, got %d", len(data))
	}
}

func TestCompactTraceResponse_PreservesPartialMetadata(t *testing.T) {
	raw := map[string]interface{}{
		"data": map[string]interface{}{
			"result": []interface{}{
				map[string]interface{}{
					"TraceId":     "abc123",
					"SpanId":      "span-1",
					"ServiceName": "svc",
					"Duration":    float64(100),
					"Timestamp":   "2025-11-02T10:00:00Z",
				},
			},
		},
		partialResultMetadataKey: map[string]interface{}{
			"partial_result":  true,
			"warning":         "Returning partial results: chunk 3/3 failed",
			"total_chunks":    3,
			"returned_traces": 1,
		},
	}

	compact := compactTraceResponse(raw)

	meta, ok := compact[partialResultMetadataKey].(map[string]interface{})
	if !ok {
		t.Fatalf("expected partial result metadata to be preserved")
	}
	if meta["partial_result"] != true {
		t.Errorf("expected partial_result=true, got %v", meta["partial_result"])
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
				TracejsonQuery: []map[string]interface{}{
					{
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

			// Verify compact response structure
			if response == nil {
				t.Fatalf("response is nil")
			}

			count := 0
			if c, ok := response["count"].(float64); ok {
				count = int(c)
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
	for i := 0; i < numTraces; i++ {
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

	exceptions, ok := response["exceptions"].([]interface{})
	if !ok {
		t.Fatalf("expected \"exceptions\" array in response, got: %T", response["exceptions"])
	}
	count := len(exceptions)
	t.Logf("Integration test successful: received %d exception(s)", count)
}
