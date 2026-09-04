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
	"last9-mcp/internal/utils"

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
	var capturedRaw []map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()

		var reqBody promReq
		_ = json.Unmarshal(body, &reqBody)
		captured = append(captured, reqBody)

		var raw map[string]interface{}
		_ = json.Unmarshal(body, &raw)
		capturedRaw = append(capturedRaw, raw)

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
		t.Fatalf("expected 2 upstream requests (event_name discovery, then range), got %d", len(captured))
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

	// get_change_events must keep using the zero-value legacy helper: this
	// query has no range-vector selector at all, so a widened step here
	// would silently drop events.
	for _, raw := range capturedRaw {
		if _, ok := raw["step"]; ok {
			t.Fatalf("range query body must not send \"step\", got: %v", raw)
		}
		if _, ok := raw["max_data_points"]; ok {
			t.Fatalf("range query body must not send \"max_data_points\", got: %v", raw)
		}
	}
}

// Filtering on the MCP spellings alone returns zero series even when matching
// events exist.
func TestGetChangeEventsHandler_FiltersUseStoredMetricLabels(t *testing.T) {
	var capturedQuery string
	capturedLabels := map[string]bool{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		var req struct {
			Query string `json:"query"`
			Label string `json:"label"`
		}
		_ = json.Unmarshal(body, &req)
		if req.Query != "" {
			capturedQuery = req.Query
		}
		if req.Label != "" {
			capturedLabels[req.Label] = true
		}
		w.Header().Set(constants.HeaderContentType, constants.HeaderContentTypeJSON)
		_, _ = io.WriteString(w, `[]`)
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
		ServiceName:     "checkout",
		Env:             "prod",
		EventName:       "deployment",
		LookbackMinutes: 30,
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !capturedLabels["event_name"] {
		t.Fatalf("available_event_names must read label \"event_name\", got %v", capturedLabels)
	}
	// event_type is never stamped, so reading it surfaces unmatchable names.
	if capturedLabels["event_type"] {
		t.Fatalf("available_event_names must not read label \"event_type\", got %v", capturedLabels)
	}
	for _, want := range []string{
		`service="checkout"`,
		`deployment_environment="prod"`,
		`event_name="deployment"`,
	} {
		if !strings.Contains(capturedQuery, want) {
			t.Fatalf("expected %s in PromQL (stored metric labels), got: %s", want, capturedQuery)
		}
	}
}

// event_name is stamped on every series, so an event_type value could never be
// matched by a filter.
func TestGetChangeEventsHandler_DiscoveryReadsEventNameOnly(t *testing.T) {
	var capturedMatches []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		w.Header().Set(constants.HeaderContentType, constants.HeaderContentTypeJSON)
		if r.URL.Path != constants.EndpointPromLabelValues {
			_, _ = io.WriteString(w, `[]`)
			return
		}
		var req struct {
			Label   string   `json:"label"`
			Matches []string `json:"matches"`
		}
		_ = json.Unmarshal(body, &req)
		capturedMatches = append(capturedMatches, req.Matches...)
		switch req.Label {
		case "event_name":
			_, _ = io.WriteString(w, `["deployment"]`)
		case "event_type":
			_, _ = io.WriteString(w, `["rollback"]`)
		default:
			_, _ = io.WriteString(w, `[]`)
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
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetChangeEventsArgs{
		LookbackMinutes: 30,
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	text := utils.GetTextContent(t, result)
	var response struct {
		AvailableEventNames []string `json:"available_event_names"`
	}
	if err := json.Unmarshal([]byte(text), &response); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	got := strings.Join(response.AvailableEventNames, ",")
	if got != "deployment" {
		t.Fatalf("available_event_names = %q, want deployment (event_type must not be unioned)", got)
	}

	// Discovery must carry the query's exclusions, or it offers names that
	// every filtered call then rejects.
	joined := strings.Join(capturedMatches, " ")
	for _, want := range []string{excludeBackupEvents, excludeScheduledSearchEvents} {
		if !strings.Contains(joined, want) {
			t.Fatalf("event_name discovery must apply %s, got matches %v", want, capturedMatches)
		}
	}
}
