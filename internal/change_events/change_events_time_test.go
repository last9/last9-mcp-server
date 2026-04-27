package change_events

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/constants"
	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestGetChangeEventsHandler_InvalidTimeOrder(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := models.Config{
		APIBaseURL: server.URL,
		TokenManager: &auth.TokenManager{
			AccessToken: "test-token",
			ExpiresAt:   time.Now().Add(24 * time.Hour),
		},
	}

	handler := NewGetChangeEventsHandler(server.Client(), cfg)
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetChangeEventsArgs{
		StartTimeISO: "2026-02-09T16:04:05Z",
		EndTimeISO:   "2026-02-09T15:04:05Z",
	})
	if err == nil {
		t.Fatalf("expected error for inverted time range, got nil")
	}
	if !strings.Contains(err.Error(), "start_time cannot be after end_time") {
		t.Fatalf("expected time-order error, got: %v", err)
	}
	if requestCount != 0 {
		t.Fatalf("expected no upstream requests on validation failure, got %d", requestCount)
	}
}

func TestGetChangeEventsHandler_ExplicitRangePrecedence(t *testing.T) {
	type promReq struct {
		Timestamp int64 `json:"timestamp"`
		Window    int64 `json:"window"`
	}
	captured := make([]promReq, 0, 2)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()

		var reqBody promReq
		_ = json.Unmarshal(body, &reqBody)
		captured = append(captured, reqBody)

		w.Header().Set(constants.HeaderContentType, constants.HeaderContentTypeJSON)
		switch r.URL.Path {
		case constants.EndpointPromLabelValues:
			_, _ = io.WriteString(w, `["deployment"]`)
		case constants.EndpointPromQuery:
			_, _ = io.WriteString(w, `[]`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	cfg := models.Config{
		APIBaseURL: server.URL,
		TokenManager: &auth.TokenManager{
			AccessToken: "test-token",
			ExpiresAt:   time.Now().Add(24 * time.Hour),
		},
	}

	handler := NewGetChangeEventsHandler(server.Client(), cfg)
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetChangeEventsArgs{
		StartTimeISO:    "2026-02-09T15:04:05Z",
		EndTimeISO:      "2026-02-09T16:04:05Z",
		LookbackMinutes: 5,
	})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if len(captured) != 2 {
		t.Fatalf("expected 2 upstream requests, got %d", len(captured))
	}

	// endTimeParam = "2026-02-09T16:04:05Z" = 1770653045
	// window = endTimeParam - startTimeParam = 3600
	for _, req := range captured {
		if req.Timestamp != 1770653045 {
			t.Fatalf("timestamp = %d, want %d (= endTimeParam)", req.Timestamp, int64(1770653045))
		}
		if req.Window != 3600 {
			t.Fatalf("window = %d, want %d", req.Window, int64(3600))
		}
	}
}
