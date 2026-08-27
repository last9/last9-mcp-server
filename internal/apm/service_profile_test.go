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
	brief, rawJSON, found := strings.Cut(text, "\n\n")
	if !found {
		t.Fatalf("want brief and JSON separated by a blank line, got:\n%s", text)
	}
	if !strings.Contains(brief, "severity_set: none") {
		t.Fatalf("brief missing severity routing:\n%s", brief)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(rawJSON), &decoded); err != nil {
		t.Fatalf("trailing payload is not valid JSON (%v):\n%s", err, rawJSON)
	}
	if decoded["service"] != "pay-svc" {
		t.Fatalf("raw JSON lost the service field: %v", decoded["service"])
	}
}

// dependencies is null in v1 and will be populated later; a shape change there
// must not take the whole tool down when the raw JSON is still usable.
func TestGetServiceProfileHandler_UnparsableBodyStillReturnsRawJSON(t *testing.T) {
	const drifted = `{"service":"pay-svc","dependencies":[{"db":"pg"}]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(drifted))
	}))
	defer server.Close()

	cfg := models.Config{APIBaseURL: server.URL}
	cfg.TokenManager = &auth.TokenManager{AccessToken: "tok", ExpiresAt: time.Now().Add(time.Hour)}

	handler := NewGetServiceProfileHandler(server.Client(), cfg)
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetServiceProfileArgs{ServiceName: "pay-svc"})
	if err != nil {
		t.Fatalf("schema drift must not fail the call: %v", err)
	}
	text := utils.GetTextContent(t, result)
	if !strings.Contains(text, drifted) {
		t.Fatalf("want raw JSON passed through, got:\n%s", text)
	}
	// A dropped brief must announce itself; silently returning bare JSON hides
	// that the routing guidance is gone.
	if !strings.Contains(text, "brief unavailable") {
		t.Fatalf("dropped brief must be marked, got:\n%s", text)
	}
}

// A null or empty body unmarshals into a zero struct, which used to render
// "Service profile: " with no name at all.
func TestGetServiceProfileHandler_EmptyBodyKeepsServiceName(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`null`))
	}))
	defer server.Close()

	cfg := models.Config{APIBaseURL: server.URL}
	cfg.TokenManager = &auth.TokenManager{AccessToken: "tok", ExpiresAt: time.Now().Add(time.Hour)}

	handler := NewGetServiceProfileHandler(server.Client(), cfg)
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetServiceProfileArgs{ServiceName: "pay-svc"})
	if err != nil {
		t.Fatalf("empty body must not fail the call: %v", err)
	}
	if text := utils.GetTextContent(t, result); !strings.Contains(text, "Service profile: pay-svc") {
		t.Fatalf("want the requested name echoed, got:\n%s", text)
	}
}

func TestGetServiceProfileHandler_ErrorBodyIsSanitized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`invalid request: {"password":"hunter2","read_url":"https://prom.example.com"}`))
	}))
	defer server.Close()

	cfg := models.Config{
		APIBaseURL:         server.URL,
		PrometheusPassword: "hunter2",
		PrometheusReadURL:  "https://prom.example.com",
	}
	cfg.TokenManager = &auth.TokenManager{AccessToken: "tok", ExpiresAt: time.Now().Add(time.Hour)}

	handler := NewGetServiceProfileHandler(server.Client(), cfg)
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetServiceProfileArgs{ServiceName: "pay-svc"})
	if err == nil {
		t.Fatal("expected error on API 400, got nil")
	}
	for _, leaked := range []string{"hunter2", "prom.example.com"} {
		if strings.Contains(err.Error(), leaked) {
			t.Fatalf("upstream error leaked %q: %v", leaked, err)
		}
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
