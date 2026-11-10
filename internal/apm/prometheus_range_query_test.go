package apm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestPromqlRangeQueryHandler(t *testing.T) {
	cfg := utils.SetupTestConfigOrSkip(t)

	handler := NewPromqlRangeQueryHandler(http.DefaultClient, *cfg)

	args := PromqlRangeQueryArgs{
		Query:        "sum(rate(http_request_duration_seconds_count[1m]))",
		StartTimeISO: time.Now().Add(-10 * time.Minute).UTC().Format(time.RFC3339),
		EndTimeISO:   time.Now().UTC().Format(time.RFC3339),
	}

	ctx := context.Background()
	req := &mcp.CallToolRequest{}
	result, _, err := handler(ctx, req, args)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	text := utils.GetTextContent(t, result)

	var details []TimeSeries
	if err := json.Unmarshal([]byte(text), &details); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
}

func TestPromqlRangeHandler_UsesLookbackAndExplicitPrecedence(t *testing.T) {
	type capturedReq struct {
		Timestamp int64 `json:"timestamp"`
		Window    int64 `json:"window"`
	}

	var captured []capturedReq
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/prom_query") {
			t.Fatalf("expected prom_query endpoint, got %s", r.URL.Path)
		}

		body, _ := io.ReadAll(r.Body)
		var reqPayload capturedReq
		_ = json.Unmarshal(body, &reqPayload)
		captured = append(captured, reqPayload)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	cfg := models.Config{
		APIBaseURL: server.URL,
		Region:     "us-east-1",
	}
	cfg.TokenManager = &auth.TokenManager{
		AccessToken: "mock-access-token",
		ExpiresAt:   time.Now().Add(365 * 24 * time.Hour),
	}

	handler := NewPromqlRangeQueryHandler(server.Client(), cfg)

	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, PromqlRangeQueryArgs{
		Query:           "sum(rate(http_request_duration_seconds_count[1m]))",
		LookbackMinutes: 30,
	})
	if err != nil {
		t.Fatalf("handler returned error for lookback mode: %v", err)
	}

	if len(captured) != 1 {
		t.Fatalf("expected 1 captured request, got %d", len(captured))
	}
	if captured[0].Window != 1800 {
		t.Fatalf("window = %d, want %d", captured[0].Window, int64(1800))
	}

	_, _, err = handler(context.Background(), &mcp.CallToolRequest{}, PromqlRangeQueryArgs{
		Query:           "sum(rate(http_request_duration_seconds_count[1m]))",
		StartTimeISO:    "2025-06-23T16:00:00Z",
		EndTimeISO:      "2025-06-23T16:10:00Z",
		LookbackMinutes: 30,
	})
	if err != nil {
		t.Fatalf("handler returned error for explicit mode: %v", err)
	}

	if len(captured) != 2 {
		t.Fatalf("expected 2 captured requests, got %d", len(captured))
	}
	if captured[1].Window != 600 {
		t.Fatalf("window with explicit timestamps = %d, want %d", captured[1].Window, int64(600))
	}
}

func TestPromqlRangeHandler_DatasourceOverride(t *testing.T) {
	type capturedReq struct {
		ReadURL  string `json:"read_url"`
		Username string `json:"username"`
		Password string `json:"password"`
	}

	var (
		captured capturedReq
		hitCount int32
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hitCount, 1)
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &captured)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	cfg := models.Config{
		APIBaseURL:         server.URL,
		PrometheusReadURL:  "https://default.example.com/prom",
		PrometheusUsername: "default-user",
		PrometheusPassword: "default-pass",
		Datasources: []models.DatasourceInfo{
			{Name: "staging", ReadURL: "https://staging.example.com/prom", Username: "staging-user", Password: "staging-pass", Region: "us-east-1"},
		},
	}
	cfg.TokenManager = &auth.TokenManager{
		AccessToken: "mock-token",
		ExpiresAt:   time.Now().Add(time.Hour),
	}

	handler := NewPromqlRangeQueryHandler(server.Client(), cfg)

	// No datasource — default credentials should reach the server
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, PromqlRangeQueryArgs{
		Query: "up", LookbackMinutes: 5,
	})
	if err != nil {
		t.Fatalf("handler error (default): %v", err)
	}
	if captured.ReadURL != "https://default.example.com/prom" {
		t.Errorf("default: read_url = %q, want default URL", captured.ReadURL)
	}

	// With datasource override — staging credentials should reach the server
	_, _, err = handler(context.Background(), &mcp.CallToolRequest{}, PromqlRangeQueryArgs{
		Query: "up", LookbackMinutes: 5, Datasource: "staging",
	})
	if err != nil {
		t.Fatalf("handler error (staging): %v", err)
	}
	if captured.ReadURL != "https://staging.example.com/prom" {
		t.Errorf("staging: read_url = %q, want staging URL", captured.ReadURL)
	}
	if captured.Username != "staging-user" {
		t.Errorf("staging: username = %q, want staging-user", captured.Username)
	}
	if captured.Password != "staging-pass" {
		t.Errorf("staging: password = %q, want staging-pass", captured.Password)
	}

	// Unknown datasource — handler must return error before hitting the server
	beforeUnknown := atomic.LoadInt32(&hitCount)
	_, _, err = handler(context.Background(), &mcp.CallToolRequest{}, PromqlRangeQueryArgs{
		Query: "up", LookbackMinutes: 5, Datasource: "nonexistent",
	})
	if err == nil {
		t.Fatal("expected error for unknown datasource, got nil")
	}
	if after := atomic.LoadInt32(&hitCount); after != beforeUnknown {
		t.Fatalf("unknown datasource: server was contacted %d time(s), want 0", after-beforeUnknown)
	}
}

func TestPromqlRangeQueryRelays400AndDrains502(t *testing.T) {
	cases := []struct {
		name       string
		status     int
		body       string
		wantSubstr string
		forbid     string
	}{
		{
			name:       "400 includes parse body",
			status:     http.StatusBadRequest,
			body:       `{"error":"parse error: unexpected identifier \"foo\""}`,
			wantSubstr: "parse error",
		},
		{
			name:       "422 too many samples gives actionable metrics error",
			status:     http.StatusUnprocessableEntity,
			body:       `{"error":"Too many samples queried. Please try selecting a smaller time range."}`,
			wantSubstr: "METRICS_QUERY_TOO_MANY_SAMPLES",
			forbid:     "Upstream response",
		},
		{
			name:       "502 omits body",
			status:     http.StatusBadGateway,
			body:       `{"error":"gateway SECRET"}`,
			wantSubstr: "HTTP 502",
			forbid:     "SECRET",
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			t.Cleanup(server.Close)

			cfg := models.Config{
				APIBaseURL: server.URL,
				Region:     "us-east-1",
				TokenManager: &auth.TokenManager{
					AccessToken: "test-token",
					ExpiresAt:   time.Now().Add(24 * time.Hour),
				},
			}
			handler := NewPromqlRangeQueryHandler(server.Client(), cfg)
			result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, PromqlRangeQueryArgs{
				Query:           "up",
				LookbackMinutes: 5,
			})
			if err != nil {
				t.Fatalf("handler returned Go error %v; want IsError result", err)
			}
			if result == nil || !result.IsError {
				t.Fatal("expected IsError result")
			}
			text := result.Content[0].(*mcp.TextContent).Text
			if !strings.Contains(text, tt.wantSubstr) {
				t.Fatalf("error %q missing %q", text, tt.wantSubstr)
			}
			if tt.forbid != "" && strings.Contains(text, tt.forbid) {
				t.Fatalf("error leaked %q: %s", tt.forbid, text)
			}
		})
	}
}
