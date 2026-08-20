package apm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/constants"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestGetServiceProfileHandler_ForwardsRegionAndService(t *testing.T) {
	var captured serviceProfileRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != constants.EndpointServiceProfile {
			t.Errorf("path %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		_ = json.NewDecoder(r.Body).Decode(&captured)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(serviceProfileResponse{
			Service: captured.Service,
			SignalShape: signalShapeResponse{
				SeveritySet: "none",
				LevelField:  "level",
			},
			Telemetry: telemetryPresenceResponse{Logs: "present", Traces: "present"},
		})
	}))
	defer server.Close()

	cfg := models.Config{
		APIBaseURL:         server.URL,
		PrometheusReadURL:  "https://prom.example.com",
		PrometheusUsername: "u",
		PrometheusPassword: "p",
		Region:             "ap-south-1",
	}
	cfg.TokenManager = &auth.TokenManager{AccessToken: "tok", ExpiresAt: time.Now().Add(time.Hour)}

	handler := NewGetServiceProfileHandler(server.Client(), cfg)
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetServiceProfileArgs{ServiceName: "pay-svc"})
	if err != nil {
		t.Fatal(err)
	}
	if captured.Service != "pay-svc" || captured.Region != "ap-south-1" || captured.ReadURL != "https://prom.example.com" {
		t.Fatalf("body %+v", captured)
	}
	text := utils.GetTextContent(t, result)
	if !strings.Contains(text, "severity_set: none") || !strings.Contains(text, "{") {
		t.Fatalf("want brief + JSON, got:\n%s", text)
	}
}

func TestGetServiceProfileHandler_EmptyServiceName(t *testing.T) {
	cfg := models.Config{APIBaseURL: "https://example.com"}
	cfg.TokenManager = &auth.TokenManager{
		AccessToken: "test-token",
		ExpiresAt:   time.Now().Add(24 * time.Hour),
	}

	handler := NewGetServiceProfileHandler(http.DefaultClient, cfg)
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetServiceProfileArgs{ServiceName: ""})
	if err == nil {
		t.Fatal("expected error for empty service_name, got nil")
	}
	if !strings.Contains(err.Error(), "service_name is required") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestGetServiceProfileHandler_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != constants.EndpointServiceProfile {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer server.Close()

	cfg := models.Config{
		APIBaseURL:         server.URL,
		PrometheusReadURL:  "https://prom.example.com",
		PrometheusUsername: "u",
		PrometheusPassword: "p",
		Region:             "ap-south-1",
	}
	cfg.TokenManager = &auth.TokenManager{
		AccessToken: "test-token",
		ExpiresAt:   time.Now().Add(24 * time.Hour),
	}

	handler := NewGetServiceProfileHandler(server.Client(), cfg)
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetServiceProfileArgs{ServiceName: "pay-svc"})
	if err == nil {
		t.Fatal("expected error on API 500, got nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("expected HTTP status in error message, got: %v", err)
	}
}
