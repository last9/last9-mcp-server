package traces

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestBuildDeviationAPIRequestUsesSameWindowForErrors(t *testing.T) {
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	request, err := buildDeviationAPIRequest(GetTraceAttributeDeviationsArgs{
		ComparisonMode: "errors", ServiceName: "last9-api", Environment: "production", LookbackMinutes: 10,
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if request.Comparison.Target != request.Comparison.Control {
		t.Fatal("errors mode must use one window for error and non-error cohorts")
	}
	if request.Comparison.Target.End.Sub(request.Comparison.Target.Start) != 10*time.Minute {
		t.Fatalf("unexpected target window: %+v", request.Comparison.Target)
	}
	if !request.Candidates.AutoDiscover || request.ContractVersion != attributeDeviationsVersion {
		t.Fatalf("unexpected request: %+v", request)
	}
}

func TestBuildDeviationAPIRequestValidatesTimeAndLatencyModes(t *testing.T) {
	timeArgs := GetTraceAttributeDeviationsArgs{
		ComparisonMode: "time", ServiceName: "checkout", Environment: "production",
		StartTimeISO: "2026-07-15T11:45:00Z", EndTimeISO: "2026-07-15T12:00:00Z",
		BaselineStartISO: "2026-07-15T11:30:00Z", BaselineEndISO: "2026-07-15T11:45:00Z",
	}
	if _, err := buildDeviationAPIRequest(timeArgs, time.Now()); err != nil {
		t.Fatal(err)
	}
	timeArgs.BaselineEndISO = "2026-07-15T11:46:00Z"
	if _, err := buildDeviationAPIRequest(timeArgs, time.Now()); err == nil || !strings.Contains(err.Error(), "equal in duration") {
		t.Fatalf("expected equal-duration error, got %v", err)
	}
	latencyArgs := GetTraceAttributeDeviationsArgs{ComparisonMode: "latency", ServiceName: "checkout", Environment: "production"}
	if _, err := buildDeviationAPIRequest(latencyArgs, time.Now()); err == nil || !strings.Contains(err.Error(), "latency_threshold_ms") {
		t.Fatalf("expected latency threshold error, got %v", err)
	}
}

func TestTraceAttributeDeviationsHandlerCallsAtomicEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != attributeDeviationsEndpoint || r.URL.Query().Get("region") != "test" {
			t.Fatalf("unexpected request URL: %s", r.URL.String())
		}
		if r.Header.Get("X-LAST9-API-TOKEN") != "Bearer test-token" {
			t.Fatalf("missing API token header")
		}
		var request deviationAPIRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.Scope.ServiceName != "last9-api" || request.Comparison.Mode != "errors" || !request.Candidates.AutoDiscover {
			t.Fatalf("unexpected payload: %+v", request)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"contract_version":"investigation-evidence/v1","analysis_version":"trace-attribute-deviations/v1"}`))
	}))
	defer server.Close()
	handler := NewGetTraceAttributeDeviationsHandler(server.Client(), deviationTestConfig(server.URL))
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetTraceAttributeDeviationsArgs{
		ComparisonMode: "errors", ServiceName: "last9-api", Environment: "production",
	})
	if err != nil {
		t.Fatal(err)
	}
	text, ok := result.Content[0].(*mcp.TextContent)
	if !ok || !strings.Contains(text.Text, `"analysis_version":"trace-attribute-deviations/v1"`) {
		t.Fatalf("unexpected MCP result: %+v", result.Content)
	}
}

func TestTraceAttributeDeviationsHandlerReturnsBackendError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusGatewayTimeout)
		w.Write([]byte(`{"code":"query_timeout","detail":"attribute deviation query timed out"}`))
	}))
	defer server.Close()
	handler := NewGetTraceAttributeDeviationsHandler(server.Client(), deviationTestConfig(server.URL))
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetTraceAttributeDeviationsArgs{
		ComparisonMode: "errors", ServiceName: "last9-api", Environment: "production",
	})
	if err == nil || !strings.Contains(err.Error(), "query_timeout") {
		t.Fatalf("expected backend timeout error, got %v", err)
	}
}

func deviationTestConfig(baseURL string) models.Config {
	return models.Config{
		APIBaseURL: baseURL,
		Region:     "test",
		TokenManager: &auth.TokenManager{
			AccessToken: "test-token",
			ExpiresAt:   time.Now().Add(time.Hour),
		},
	}
}
