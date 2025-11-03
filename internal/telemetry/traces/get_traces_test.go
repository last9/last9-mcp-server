package traces

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var (
	testBaseURL      = "https://otlp-aps1.last9.io:443"
	testAuthToken    = os.Getenv("TEST_AUTH_TOKEN")
	testRefreshToken = os.Getenv("TEST_REFRESH_TOKEN")
)

func TestValidateGetTracesArgs(t *testing.T) {
	tests := []struct {
		name    string
		args    GetTracesArgs
		wantErr bool
		errMsg  string
	}{
		{
			name:    "Both trace_id and service_name empty",
			args:    GetTracesArgs{},
			wantErr: true,
			errMsg:  "either trace_id or service_name must be provided",
		},
		{
			name: "Both trace_id and service_name provided",
			args: GetTracesArgs{
				TraceID:     "abc123",
				ServiceName: "test-service",
			},
			wantErr: true,
			errMsg:  "cannot specify both trace_id and service_name",
		},
		{
			name: "Only trace_id provided - valid",
			args: GetTracesArgs{
				TraceID: "abc123def456",
			},
			wantErr: false,
		},
		{
			name: "Only service_name provided - valid",
			args: GetTracesArgs{
				ServiceName: "test-service",
			},
			wantErr: false,
		},
		{
			name: "Invalid lookback_minutes - too small",
			args: GetTracesArgs{
				ServiceName:     "test-service",
				LookbackMinutes: 0.5,
			},
			wantErr: true,
			errMsg:  "lookback_minutes must be between 1 and 1440",
		},
		{
			name: "Invalid lookback_minutes - too large",
			args: GetTracesArgs{
				ServiceName:     "test-service",
				LookbackMinutes: 1500,
			},
			wantErr: true,
			errMsg:  "lookback_minutes must be between 1 and 1440",
		},
		{
			name: "Invalid limit - too small",
			args: GetTracesArgs{
				ServiceName: "test-service",
				Limit:       0.5,
			},
			wantErr: true,
			errMsg:  "limit must be between 1 and 100",
		},
		{
			name: "Invalid limit - too large",
			args: GetTracesArgs{
				ServiceName: "test-service",
				Limit:       150,
			},
			wantErr: true,
			errMsg:  "limit must be between 1 and 100",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateGetTracesArgs(tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateGetTracesArgs() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil && tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
				t.Errorf("validateGetTracesArgs() error = %v, want error containing %v", err, tt.errMsg)
			}
		})
	}
}

func TestParseGetTracesParams(t *testing.T) {
	cfg := models.Config{
		BaseURL: testBaseURL,
	}

	tests := []struct {
		name      string
		args      GetTracesArgs
		wantErr   bool
		wantTrace string
		wantSvc   string
		wantLimit int
	}{
		{
			name: "Valid trace ID request",
			args: GetTracesArgs{
				TraceID: "abc123def456",
				Limit:   20,
			},
			wantErr:   false,
			wantTrace: "abc123def456",
			wantLimit: 20,
		},
		{
			name: "Valid service name request with defaults",
			args: GetTracesArgs{
				ServiceName: "payment-service",
			},
			wantErr:   false,
			wantSvc:   "payment-service",
			wantLimit: LimitDefault,
		},
		{
			name: "Valid service name with custom params",
			args: GetTracesArgs{
				ServiceName:     "api-service",
				LookbackMinutes: 30,
				Limit:           5,
				Env:             "prod",
			},
			wantErr:   false,
			wantSvc:   "api-service",
			wantLimit: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseGetTracesParams(tt.args, cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseGetTracesParams() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err == nil {
				if result.TraceID != tt.wantTrace {
					t.Errorf("parseGetTracesParams() TraceID = %v, want %v", result.TraceID, tt.wantTrace)
				}
				if result.ServiceName != tt.wantSvc {
					t.Errorf("parseGetTracesParams() ServiceName = %v, want %v", result.ServiceName, tt.wantSvc)
				}
				if result.Limit != tt.wantLimit {
					t.Errorf("parseGetTracesParams() Limit = %v, want %v", result.Limit, tt.wantLimit)
				}
			}
		})
	}
}

func TestBuildGetTracesFilters(t *testing.T) {
	tests := []struct {
		name     string
		params   *GetTracesQueryParams
		wantLen  int
		wantCond string
	}{
		{
			name: "Trace ID filter",
			params: &GetTracesQueryParams{
				TraceID: "abc123def456",
			},
			wantLen:  1,
			wantCond: "TraceId",
		},
		{
			name: "Service name filter",
			params: &GetTracesQueryParams{
				ServiceName: "test-service",
			},
			wantLen:  1,
			wantCond: "ServiceName",
		},
		{
			name: "Service name with environment filter",
			params: &GetTracesQueryParams{
				ServiceName: "test-service",
				Env:         "prod",
			},
			wantLen:  2,
			wantCond: "ServiceName",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filters := buildGetTracesFilters(tt.params)
			if len(filters) != tt.wantLen {
				t.Errorf("buildGetTracesFilters() len = %v, want %v", len(filters), tt.wantLen)
			}

			// Check that the main filter condition exists
			found := false
			for _, filter := range filters {
				if eq, ok := filter["$eq"].([]interface{}); ok && len(eq) >= 2 {
					if field, ok := eq[0].(string); ok && field == tt.wantCond {
						found = true
						break
					}
				}
			}
			if !found {
				t.Errorf("buildGetTracesFilters() missing expected condition %v", tt.wantCond)
			}
		})
	}
}

func TestGetTracesHandler_MockedResponse(t *testing.T) {
	// Mock API response
	mockResponse := `{
		"data": {
			"result": [
				{
					"TraceId": "abc123def456",
					"SpanId": "span789",
					"SpanKind": "SPAN_KIND_SERVER",
					"SpanName": "GET /api/users",
					"ServiceName": "api-service",
					"Duration": 150000000,
					"Timestamp": "2025-11-02T10:00:00Z",
					"TraceState": "",
					"StatusCode": "STATUS_CODE_OK"
				},
				{
					"TraceId": "def789ghi012",
					"SpanId": "span456",
					"SpanKind": "SPAN_KIND_CLIENT",
					"SpanName": "db_query",
					"ServiceName": "api-service",
					"Duration": 25000000,
					"Timestamp": "2025-11-02T10:00:01Z",
					"TraceState": "",
					"StatusCode": "STATUS_CODE_OK"
				}
			]
		}
	}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request method and path
		if r.Method != "POST" {
			t.Errorf("Expected POST request, got %s", r.Method)
		}
		if !strings.Contains(r.URL.Path, "/cat/api/traces/v2/query_range/json") {
			t.Errorf("Expected traces API path, got %s", r.URL.Path)
		}

		// Verify request body contains expected filters
		body, _ := io.ReadAll(r.Body)
		var req TraceQueryRequest
		json.Unmarshal(body, &req)

		if len(req.Pipeline) == 0 {
			t.Error("Expected pipeline in request")
		}

		w.WriteHeader(http.StatusOK)
		io.WriteString(w, mockResponse)
	}))
	defer server.Close()

	cfg := models.Config{
		APIBaseURL:   server.URL,
		BaseURL:      testBaseURL,
		AuthToken:    "test-token",
		RefreshToken: testRefreshToken,
	}

	handler := GetTracesHandler(server.Client(), cfg)

	tests := []struct {
		name    string
		args    GetTracesArgs
		wantErr bool
	}{
		{
			name: "Get traces by service name",
			args: GetTracesArgs{
				ServiceName:     "api-service",
				LookbackMinutes: 60,
				Limit:           10,
			},
			wantErr: false,
		},
		{
			name: "Get traces by trace ID",
			args: GetTracesArgs{
				TraceID: "abc123def456",
				Limit:   5,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			req := &mcp.CallToolRequest{}
			result, _, err := handler(ctx, req, tt.args)

			if (err != nil) != tt.wantErr {
				t.Errorf("GetTracesHandler() error = %v, wantErr %v", err, tt.wantErr)
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

				var traceResponse TraceQueryResponse
				if err := json.Unmarshal([]byte(textContent.Text), &traceResponse); err != nil {
					t.Fatalf("failed to unmarshal response: %v", err)
				}

				if !traceResponse.Success {
					t.Errorf("expected successful response, got success=%v", traceResponse.Success)
				}

				if len(traceResponse.Data) == 0 {
					t.Error("expected trace data in response")
				}

				// Verify trace data structure
				for _, trace := range traceResponse.Data {
					if trace.TraceID == "" {
						t.Error("expected TraceID to be populated")
					}
					if trace.ServiceName == "" {
						t.Error("expected ServiceName to be populated")
					}
				}
			}
		})
	}
}

func TestGetTracesHandler_ValidationErrors(t *testing.T) {
	cfg := models.Config{
		BaseURL:   testBaseURL,
		AuthToken: "test-token",
	}

	handler := GetTracesHandler(http.DefaultClient, cfg)

	tests := []struct {
		name    string
		args    GetTracesArgs
		wantErr bool
		errMsg  string
	}{
		{
			name:    "Missing both trace_id and service_name",
			args:    GetTracesArgs{},
			wantErr: true,
			errMsg:  "either trace_id or service_name must be provided",
		},
		{
			name: "Both trace_id and service_name provided",
			args: GetTracesArgs{
				TraceID:     "abc123",
				ServiceName: "test-service",
			},
			wantErr: true,
			errMsg:  "cannot specify both trace_id and service_name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			req := &mcp.CallToolRequest{}
			_, _, err := handler(ctx, req, tt.args)

			if (err != nil) != tt.wantErr {
				t.Errorf("GetTracesHandler() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if err != nil && tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
				t.Errorf("GetTracesHandler() error = %v, want error containing %v", err, tt.errMsg)
			}
		})
	}
}

// Integration test - requires real API credentials
func TestGetTracesHandler_Integration(t *testing.T) {
	if testAuthToken == "" || testRefreshToken == "" {
		t.Skip("Skipping integration test: TEST_AUTH_TOKEN or TEST_REFRESH_TOKEN not set")
	}

	cfg := models.Config{
		BaseURL:      testBaseURL,
		AuthToken:    testAuthToken,
		RefreshToken: testRefreshToken,
	}

	if err := utils.PopulateAPICfg(&cfg); err != nil {
		t.Fatalf("failed to refresh access token: %v", err)
	}

	handler := GetTracesHandler(http.DefaultClient, cfg)

	// Test with service name
	args := GetTracesArgs{
		ServiceName:     "test-service", // Replace with actual service name for real testing
		LookbackMinutes: 60,
		Limit:           5,
	}

	ctx := context.Background()
	req := &mcp.CallToolRequest{}
	result, _, err := handler(ctx, req, args)

	if err != nil {
		// Integration test may fail if service doesn't exist - that's ok
		t.Logf("Integration test failed (expected if test service doesn't exist): %v", err)
		return
	}

	if len(result.Content) == 0 {
		t.Fatalf("expected content in result")
	}

	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent type")
	}

	var traceResponse TraceQueryResponse
	if err := json.Unmarshal([]byte(textContent.Text), &traceResponse); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	t.Logf("Integration test successful: received %d traces", len(traceResponse.Data))
}
