package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"last9-mcp/internal/last9api"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const alertingNotEnabledMessage = "Alerting is not enabled for this org, so there is no alert data. Enable alerting in the Last9 app to use alert tools."

func TestAlertToolsReturnAlertingNotEnabledAsValidEmptyState(t *testing.T) {
	tests := []struct {
		tool string
		path string
		args map[string]any
	}{
		{tool: "get_alerts", path: "/alerts/monitor"},
		{tool: "get_alert_config", path: "/alert-rules"},
		{tool: "get_alert_groups", path: "/entities/list"},
		{tool: "get_alert_rule_state", path: "/alerts/monitor", args: map[string]any{
			"start_time": "2026-11-13T10:00:00Z", "end_time": "2026-11-13T10:01:00Z", "step": 60,
		}},
		{tool: "get_entity_alert_rules", path: "/entities/group-1/alert-rules", args: map[string]any{"entity_id": "group-1"}},
		{tool: "get_alert_coverage", path: "/alerts/coverage"},
		{tool: "get_alert_hygiene", path: "/alerts/hygiene"},
		{tool: "get_alert_operations_summary", path: "/alerts/health_summary", args: map[string]any{
			"start_time_iso": "2026-11-13T10:00:00Z", "end_time_iso": "2026-11-13T11:00:00Z",
		}},
	}

	for _, tt := range tests {
		t.Run(tt.tool, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tt.tool == "get_alert_config" && r.URL.Path == "/notification_settings" {
					_, _ = io.WriteString(w, `[]`)
					return
				}
				if r.URL.Path != tt.path {
					t.Errorf("path = %q, want %q", r.URL.Path, tt.path)
					http.Error(w, "unexpected path", http.StatusNotFound)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnprocessableEntity)
				_, _ = io.WriteString(w, `{"error":"Alerting is not enabled for this organization.","error_details":{"code":"alerting_not_enabled"},"status":422}`)
			}))
			t.Cleanup(upstream.Close)

			session := connectTestSession(t, writeToolTestConfig(upstream.URL), upstream.Client())

			result := callRegisteredTool(t, session, &mcp.CallToolParams{Name: tt.tool, Arguments: tt.args})
			if result.IsError {
				t.Fatalf("typed disabled response returned an error: %q", toolResultText(result))
			}
			if got := toolResultText(result); got != alertingNotEnabledMessage {
				t.Fatalf("text = %q, want %q", got, alertingNotEnabledMessage)
			}
		})
	}
}

func TestAlertToolOtherUpstreamErrorsAreUnchanged(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
	}{
		{name: "unauthorized", status: http.StatusUnauthorized, body: `{"error_details":{"code":"alerting_not_enabled"}}`},
		{name: "forbidden", status: http.StatusForbidden, body: `{"error_details":{"code":"alerting_not_enabled"}}`},
		{name: "top-level code", status: http.StatusUnprocessableEntity, body: `{"code":"alerting_not_enabled"}`},
		{name: "untyped 4xx", status: http.StatusFailedDependency, body: `{"error":{"code":"some_other_code"}}`},
		{name: "server error", status: http.StatusInternalServerError, body: `{"error":{"code":"alerting_not_enabled"}}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = io.WriteString(w, tt.body)
			}))
			t.Cleanup(upstream.Close)
			cfg := writeToolTestConfig(upstream.URL)
			handler := func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
				err := last9api.NewClient(upstream.Client(), cfg).Do(ctx, "alerts", last9api.Request{Method: http.MethodGet, Path: "/alerts/monitor"}, nil)
				return nil, nil, err
			}
			session := connectToolFixture(t, registeredToolFixture{
				tool:    &mcp.Tool{Name: "get_alerts", Annotations: readOnlyTool("Get Alerts")},
				timeout: time.Second,
			}, handler)

			result := callRegisteredTool(t, session, &mcp.CallToolParams{Name: "get_alerts", Arguments: map[string]any{}})
			if !result.IsError {
				t.Fatalf("HTTP %d returned a successful result: %q", tt.status, toolResultText(result))
			}
			if text := toolResultText(result); !strings.Contains(text, "HTTP "+strconv.Itoa(tt.status)) {
				t.Fatalf("original status missing from %q", text)
			}
		})
	}
}

func TestNonAlertToolDoesNotConvertAlertingNotEnabled(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = io.WriteString(w, `{"error_details":{"code":"alerting_not_enabled"}}`)
	}))
	t.Cleanup(upstream.Close)
	cfg := writeToolTestConfig(upstream.URL)
	handler := func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		err := last9api.NewClient(upstream.Client(), cfg).Do(ctx, "logs", last9api.Request{Method: http.MethodGet, Path: "/logs"}, nil)
		return nil, nil, err
	}
	session := connectToolFixture(t, registeredToolFixture{
		tool:    &mcp.Tool{Name: "get_logs", Annotations: readOnlyTool("Get Logs")},
		timeout: time.Second,
	}, handler)

	result := callRegisteredTool(t, session, &mcp.CallToolParams{Name: "get_logs", Arguments: map[string]any{}})
	if !result.IsError {
		t.Fatalf("non-alert tool returned a successful result: %q", toolResultText(result))
	}
	if text := toolResultText(result); !strings.Contains(text, "HTTP 422") {
		t.Fatalf("original status missing from %q", text)
	}
}
