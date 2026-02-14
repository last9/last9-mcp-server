package alerting

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/constants"
	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestGetAlertsHandler_TimeISOPrecedence(t *testing.T) {
	expectedUnix := int64(1770649445) // 2026-02-09T15:04:05Z
	var receivedTimestamp string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedTimestamp = r.URL.Query().Get("timestamp")
		w.Header().Set(constants.HeaderContentType, constants.HeaderContentTypeJSON)
		_, _ = io.WriteString(w, `{"timestamp":1770649445,"window":900,"alert_rules":[]}`)
	}))
	defer server.Close()

	cfg := models.Config{
		APIBaseURL: server.URL,
		TokenManager: &auth.TokenManager{
			AccessToken: "test-token",
			ExpiresAt:   time.Now().Add(24 * time.Hour),
		},
	}

	handler := NewGetAlertsHandler(server.Client(), cfg)
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetAlertsArgs{
		TimeISO:   "2026-02-09T15:04:05Z",
		Timestamp: 1111111111, // deprecated alias should be ignored when time_iso is present
		Window:    900,
	})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if receivedTimestamp != "1770649445" {
		t.Fatalf("expected timestamp query param %d, got %s", expectedUnix, receivedTimestamp)
	}
}

func TestGetAlertsHandler_InvalidTimeISO(t *testing.T) {
	cfg := models.Config{
		APIBaseURL: "http://example.com",
		TokenManager: &auth.TokenManager{
			AccessToken: "test-token",
			ExpiresAt:   time.Now().Add(24 * time.Hour),
		},
	}

	handler := NewGetAlertsHandler(http.DefaultClient, cfg)
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetAlertsArgs{
		TimeISO: "2026/02/09 15:04:05",
	})
	if err == nil {
		t.Fatalf("expected error for invalid time_iso, got nil")
	}
}
