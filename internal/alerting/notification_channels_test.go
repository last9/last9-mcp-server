package alerting

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

// Integration test — skipped if TEST_REFRESH_TOKEN is not set.
func TestGetNotificationChannelsHandler_Integration(t *testing.T) {
	cfg := utils.SetupTestConfigOrSkip(t)
	handler := NewGetNotificationChannelsHandler(http.DefaultClient, *cfg)

	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetNotificationChannelsArgs{})
	if utils.CheckAPIError(t, err) {
		return
	}

	text := utils.GetTextContent(t, result)
	if !strings.Contains(text, "id\tname\ttype\t") {
		t.Fatalf("expected TSV header row in response, got:\n%s", text)
	}
	t.Logf("Integration test successful:\n%s", text)
}

func TestGetNotificationChannelsHandler_TSVFormat(t *testing.T) {
	boolTrue := true
	channels := []NotificationChannel{
		{
			ID:           1,
			Name:         "ops\t\n\r-slack",
			Type:         "slack",
			Global:       true,
			InUse:        true,
			SendResolved: &boolTrue,
			Severity:     "critical",
			Priority:     1,
		},
	}

	text, _, err := executeGetNotificationChannels(t, channels, http.StatusOK)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(text), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines (count + header + row), got %d:\n%s", len(lines), text)
	}

	// Count line
	if !strings.Contains(lines[0], "Found 1 notification channel(s)") {
		t.Fatalf("unexpected count line: %s", lines[0])
	}

	// Header
	wantHeader := "id\tname\ttype\tglobal\tin_use\tsend_resolved\tsnoozed_until\tseverity\tpriority\tservices"
	if lines[1] != wantHeader {
		t.Fatalf("header mismatch\ngot:  %s\nwant: %s", lines[1], wantHeader)
	}

	rows := assertStableNotificationChannelsTSV(t, text, len(channels))
	cols := rows[0]
	if cols[0] != "1" || cols[1] != "ops\\t\\n\\r-slack" || cols[2] != "slack" {
		t.Fatalf("unexpected column values: %v", cols)
	}
	if cols[5] != "true" {
		t.Fatalf("send_resolved: got %q, want %q", cols[5], "true")
	}
	if cols[6] != "-" {
		t.Fatalf("snoozed_until: got %q, want %q (not snoozed)", cols[6], "-")
	}
	if cols[9] != "-" {
		t.Fatalf("services: got %q, want %q (global, no services)", cols[9], "-")
	}
}

func TestGetNotificationChannelsHandler_SendResolvedVariants(t *testing.T) {
	boolTrue := true
	boolFalse := false

	tests := []struct {
		name         string
		sendResolved *bool
		wantCol      string
	}{
		{"null (not configured)", nil, "null"},
		{"explicitly true", &boolTrue, "true"},
		{"explicitly false", &boolFalse, "false"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			channels := []NotificationChannel{
				{ID: 1, Name: "ch", Type: "slack", SendResolved: tt.sendResolved},
			}
			text, _, err := executeGetNotificationChannels(t, channels, http.StatusOK)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			rows := assertStableNotificationChannelsTSV(t, text, len(channels))
			cols := rows[0]
			if cols[5] != tt.wantCol {
				t.Fatalf("send_resolved col: got %q, want %q", cols[5], tt.wantCol)
			}
		})
	}
}

func TestGetNotificationChannelsHandler_SnoozedUntil(t *testing.T) {
	snoozeTS := int64(1700000000)
	channels := []NotificationChannel{
		{ID: 1, Name: "ch", Type: "pagerduty", SnoozeUntil: &snoozeTS},
	}

	text, _, err := executeGetNotificationChannels(t, channels, http.StatusOK)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rows := assertStableNotificationChannelsTSV(t, text, len(channels))
	cols := rows[0]

	want := time.Unix(snoozeTS, 0).UTC().Format("2006-01-02 15:04:05 UTC")
	if cols[6] != want {
		t.Fatalf("snoozed_until: got %q, want %q", cols[6], want)
	}
}

func TestGetNotificationChannelsHandler_Services(t *testing.T) {
	tests := []struct {
		name     string
		services []notificationChannelService
		wantCol  string
	}{
		{
			name:     "no services (global)",
			services: nil,
			wantCol:  "-",
		},
		{
			name:     "service without namespace",
			services: []notificationChannelService{{Name: "payments"}},
			wantCol:  "payments",
		},
		{
			name:     "service with namespace",
			services: []notificationChannelService{{Name: "api", Namespace: "prod"}},
			wantCol:  "prod/api",
		},
		{
			name: "service with control characters",
			services: []notificationChannelService{
				{Name: "api\tteam", Namespace: "prod\ncore"},
				{Name: "worker\rjobs"},
			},
			wantCol: "prod\\ncore/api\\tteam,worker\\rjobs",
		},
		{
			name: "multiple services",
			services: []notificationChannelService{
				{Name: "api", Namespace: "prod"},
				{Name: "worker"},
			},
			wantCol: "prod/api,worker",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			channels := []NotificationChannel{
				{ID: 1, Name: "ch", Type: "slack", Services: tt.services},
			}
			text, _, err := executeGetNotificationChannels(t, channels, http.StatusOK)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			rows := assertStableNotificationChannelsTSV(t, text, len(channels))
			cols := rows[0]
			if cols[9] != tt.wantCol {
				t.Fatalf("services col: got %q, want %q", cols[9], tt.wantCol)
			}
		})
	}
}

func TestGetNotificationChannelsHandler_EmptyResponse(t *testing.T) {
	text, _, err := executeGetNotificationChannels(t, []NotificationChannel{}, http.StatusOK)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(text, "Found 0 notification channel(s)") {
		t.Fatalf("expected zero-count message, got:\n%s", text)
	}

	lines := strings.Split(strings.TrimSpace(text), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines (count + header), got %d:\n%s", len(lines), text)
	}
}

func TestGetNotificationChannelsHandler_APIError(t *testing.T) {
	_, _, err := executeGetNotificationChannels(t, nil, http.StatusInternalServerError)
	if err == nil {
		t.Fatal("expected error for non-200 API response")
	}
	if !strings.Contains(err.Error(), "API request failed with status 500") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGetNotificationChannelsHandler_APIErrorTruncatesBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(strings.Repeat("x", maxNotificationChannelsErrorBodyBytes+32) + "END"))
	}))
	defer server.Close()

	cfg := newTestNotificationChannelsConfig(server.URL)
	handler := NewGetNotificationChannelsHandler(server.Client(), cfg)
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetNotificationChannelsArgs{})
	if err == nil {
		t.Fatal("expected error for non-200 API response")
	}
	if !strings.Contains(err.Error(), "...(truncated)") {
		t.Fatalf("expected truncated marker in error, got: %v", err)
	}
	if strings.Contains(err.Error(), "END") {
		t.Fatalf("expected truncated body to omit tail marker, got: %v", err)
	}
}

func TestGetNotificationChannelsHandler_MissingTokenManager(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()

	cfg := newTestNotificationChannelsConfig(server.URL)
	cfg.TokenManager = nil

	handler := NewGetNotificationChannelsHandler(server.Client(), cfg)
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetNotificationChannelsArgs{})
	if err == nil {
		t.Fatal("expected error when token manager is missing")
	}
	if !strings.Contains(err.Error(), "token manager is not configured") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGetNotificationChannelsHandler_EmptyAccessToken(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()

	cfg := newTestNotificationChannelsConfig(server.URL)
	cfg.TokenManager = &auth.TokenManager{
		AccessToken: "   ",
		ExpiresAt:   time.Now().Add(24 * time.Hour),
	}

	handler := NewGetNotificationChannelsHandler(server.Client(), cfg)
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetNotificationChannelsArgs{})
	if err == nil {
		t.Fatal("expected error when access token is empty")
	}
	if !strings.Contains(err.Error(), "access token cannot be empty") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGetNotificationChannelsHandler_DeepLink(t *testing.T) {
	_, result, err := executeGetNotificationChannels(t, []NotificationChannel{}, http.StatusOK)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	refURL, ok := result.Meta["reference_url"].(string)
	if !ok || refURL == "" {
		t.Fatalf("expected reference_url in meta, got: %v", result.Meta)
	}
	if !strings.Contains(refURL, "settings/notification-channels") {
		t.Fatalf("deep link %q does not point to notification channels page", refURL)
	}
}

// executeGetNotificationChannels spins up a mock API server, calls the handler, and returns
// the text response, the full result, and any error.
func executeGetNotificationChannels(
	t *testing.T,
	channels []NotificationChannel,
	statusCode int,
) (string, *mcp.CallToolResult, error) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != constants.EndpointNotificationSettings {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		if statusCode == http.StatusOK {
			_ = json.NewEncoder(w).Encode(channels)
		} else {
			_, _ = w.Write([]byte(`{"error":"internal error"}`))
		}
	}))
	defer server.Close()

	cfg := newTestNotificationChannelsConfig(server.URL)

	handler := NewGetNotificationChannelsHandler(server.Client(), cfg)
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetNotificationChannelsArgs{})
	if err != nil {
		return "", result, err
	}

	return utils.GetTextContent(t, result), result, nil
}

func newTestNotificationChannelsConfig(apiBaseURL string) models.Config {
	cfg := models.Config{
		APIBaseURL: apiBaseURL,
		OrgSlug:    "test-org",
		ClusterID:  "cluster-1",
	}
	cfg.TokenManager = &auth.TokenManager{
		AccessToken: "test-token",
		ExpiresAt:   time.Now().Add(24 * time.Hour),
	}

	return cfg
}

func assertStableNotificationChannelsTSV(t *testing.T, text string, wantDataRows int) [][]string {
	t.Helper()

	lines := strings.Split(strings.TrimSpace(text), "\n")
	if len(lines) != wantDataRows+2 {
		t.Fatalf("expected %d lines (count + header + %d row(s)), got %d:\n%s", wantDataRows+2, wantDataRows, len(lines), text)
	}

	rows := make([][]string, 0, wantDataRows)
	for i, line := range lines[2:] {
		cols := strings.Split(line, "\t")
		if len(cols) != 10 {
			t.Fatalf("row %d: expected 10 TSV columns, got %d: %v", i+1, len(cols), cols)
		}
		for _, col := range cols {
			if strings.ContainsAny(col, "\n\r") {
				t.Fatalf("row %d contains raw newline or carriage return in column %q", i+1, col)
			}
		}
		rows = append(rows, cols)
	}

	return rows
}
