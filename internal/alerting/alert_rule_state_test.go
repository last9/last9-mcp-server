package alerting

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func newAlertRuleStateTestConfig(serverURL string) models.Config {
	return models.Config{
		APIBaseURL: serverURL,
		OrgSlug:    "test-org",
		TokenManager: &auth.TokenManager{
			AccessToken: "mock-token",
			ExpiresAt:   time.Now().Add(365 * 24 * time.Hour),
		},
	}
}

type alertRuleStateDatapoint struct {
	Timestamp int64 `json:"timestamp"`
	IsFiring  int   `json:"is_firing"`
}

func decodeAlertRuleStateResult(t *testing.T, result *mcp.CallToolResult) map[string][]alertRuleStateDatapoint {
	t.Helper()
	if len(result.Content) == 0 {
		t.Fatalf("Expected content in result")
	}
	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("Expected TextContent")
	}
	var decoded map[string][]alertRuleStateDatapoint
	if err := json.Unmarshal([]byte(textContent.Text), &decoded); err != nil {
		t.Fatalf("Expected JSON output, got error: %v\npayload: %s", err, textContent.Text)
	}
	return decoded
}

func TestAlertRuleStateHandler_GroupsRulesAcrossTimestamps(t *testing.T) {
	const (
		startTime = int64(1718000000)
		endTime   = int64(1718000060)
		step      = int64(60)
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("Expected GET request, got %s", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/alerts/monitor") {
			t.Errorf("Expected path to end with /alerts/monitor, got %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Query().Get("timestamp") {
		case "1718000000":
			w.Write([]byte(`{
				"timestamp": 1718000000,
				"window": 60,
				"alert_rules": [
					{"rule_id": "test-rule-1", "state": "firing"},
					{"rule_id": "test-rule-2", "state": "normal"}
				]
			}`))
		case "1718000060":
			w.Write([]byte(`{
				"timestamp": 1718000060,
				"window": 60,
				"alert_rules": [
					{"rule_id": "test-rule-1", "state": "pending"},
					{"rule_id": "test-rule-2", "state": "firing"}
				]
			}`))
		default:
			t.Errorf("Unexpected timestamp in request: %s", r.URL.Query().Get("timestamp"))
		}
	}))
	defer server.Close()

	handler := NewAlertRuleStateHandler(server.Client(), newAlertRuleStateTestConfig(server.URL))

	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, AlertRuleStateRequest{
		StartTime: startTime,
		EndTime:   endTime,
		Step:      step,
	})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("Unexpected IsError result: %+v", result.Content)
	}

	decoded := decodeAlertRuleStateResult(t, result)

	rule1, ok := decoded["test-rule-1"]
	if !ok {
		t.Fatalf("Expected test-rule-1 in output, got keys: %v", mapKeys(decoded))
	}
	if len(rule1) != 2 {
		t.Fatalf("Expected 2 datapoints for test-rule-1, got %d", len(rule1))
	}
	if rule1[0].Timestamp != startTime || rule1[0].IsFiring != 1 {
		t.Errorf("Expected rule-1 datapoint[0] = firing at %d, got %+v", startTime, rule1[0])
	}
	if rule1[1].Timestamp != endTime || rule1[1].IsFiring != 0 {
		t.Errorf("Expected rule-1 datapoint[1] = not firing (pending) at %d, got %+v", endTime, rule1[1])
	}

	rule2, ok := decoded["test-rule-2"]
	if !ok {
		t.Fatalf("Expected test-rule-2 in output, got keys: %v", mapKeys(decoded))
	}
	if rule2[0].IsFiring != 0 || rule2[1].IsFiring != 1 {
		t.Errorf("Expected rule-2 to flip from 0 to 1, got %+v", rule2)
	}
}

func TestAlertRuleStateHandler_ForwardsFilters(t *testing.T) {
	var captured url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"timestamp": 1718000000, "window": 60, "alert_rules": []}`))
	}))
	defer server.Close()

	handler := NewAlertRuleStateHandler(server.Client(), newAlertRuleStateTestConfig(server.URL))

	args := AlertRuleStateRequest{
		AlertGroupID:   "group-7",
		RuleName:       ".*latency.*",
		AlertGroupName: ".*api.*",
		LabelFilters:   "env=prod,team=core",
		State:          "firing",
		StartTime:      1718000000,
		EndTime:        1718000000 + 60,
		Step:           60,
	}

	if _, _, err := handler(context.Background(), &mcp.CallToolRequest{}, args); err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	expected := map[string]string{
		"alert_group_id":   "group-7",
		"rule_name":        ".*latency.*",
		"alert_group_name": ".*api.*",
		"label_filters":    "env=prod,team=core",
		"state":            "firing",
	}
	for key, want := range expected {
		if got := captured.Get(key); got != want {
			t.Errorf("Expected query param %s=%q, got %q", key, want, got)
		}
	}
}

func TestAlertRuleStateHandler_OmitsAbsentFilters(t *testing.T) {
	var captured url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"timestamp": 1, "window": 60, "alert_rules": []}`))
	}))
	defer server.Close()

	handler := NewAlertRuleStateHandler(server.Client(), newAlertRuleStateTestConfig(server.URL))

	// No filters set — only the always-present timestamp/window should be in the query.
	if _, _, err := handler(context.Background(), &mcp.CallToolRequest{}, AlertRuleStateRequest{
		StartTime: 1, EndTime: 61, Step: 60,
	}); err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	for _, key := range []string{"alert_group_id", "rule_name", "alert_group_name", "label_filters", "state"} {
		if captured.Has(key) {
			t.Errorf("Expected %s to be absent from query when filter not set, got %q", key, captured.Get(key))
		}
	}

	for _, key := range []string{"timestamp", "window"} {
		if !captured.Has(key) {
			t.Errorf("Expected %s to always be present in query", key)
		}
	}
}

func TestAlertRuleStateHandler_SetsAuthHeader(t *testing.T) {
	var sawXLast9, sawAuthorization bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-LAST9-API-TOKEN") != "" {
			sawXLast9 = true
		}
		if r.Header.Get("Authorization") != "" {
			sawAuthorization = true
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"timestamp": 1, "window": 60, "alert_rules": []}`))
	}))
	defer server.Close()

	handler := NewAlertRuleStateHandler(server.Client(), newAlertRuleStateTestConfig(server.URL))

	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, AlertRuleStateRequest{
		StartTime: 1, EndTime: 61, Step: 60,
	})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if !sawXLast9 {
		t.Errorf("Expected X-LAST9-API-TOKEN header to be set")
	}
	if sawAuthorization {
		t.Errorf("Authorization header should not be set; codebase convention is X-LAST9-API-TOKEN only")
	}
}

func TestAlertRuleStateHandler_ValidationErrors(t *testing.T) {
	handler := NewAlertRuleStateHandler(http.DefaultClient, newAlertRuleStateTestConfig("http://unused"))

	cases := []struct {
		name string
		args AlertRuleStateRequest
		want string
	}{
		{
			name: "start equals end",
			args: AlertRuleStateRequest{StartTime: 100, EndTime: 100, Step: 60},
			want: "start_time must be less than end_time",
		},
		{
			name: "non-positive step",
			args: AlertRuleStateRequest{StartTime: 100, EndTime: 200, Step: 0},
			want: "step must be greater than 0",
		},
		{
			name: "too many points",
			args: AlertRuleStateRequest{StartTime: 0, EndTime: 60 * 200, Step: 60},
			want: "too many points",
		},
		{
			name: "cap boundary - 101 samples is rejected",
			args: AlertRuleStateRequest{StartTime: 0, EndTime: 60 * 100, Step: 60},
			want: "too many points",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, tc.args)
			if err != nil {
				t.Fatalf("Expected IsError result, got transport error: %v", err)
			}
			if result == nil || !result.IsError {
				t.Fatalf("Expected IsError=true, got %+v", result)
			}
			text := result.Content[0].(*mcp.TextContent).Text
			if !strings.Contains(text, tc.want) {
				t.Errorf("Expected message containing %q, got %q", tc.want, text)
			}
		})
	}
}

func TestAlertRuleStateHandler_APIError(t *testing.T) {
	// Reserve a port by binding then closing the listener — guarantees connection-refused.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to reserve port: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	handler := NewAlertRuleStateHandler(&http.Client{Timeout: 2 * time.Second}, newAlertRuleStateTestConfig("http://"+addr))

	_, _, err = handler(context.Background(), &mcp.CallToolRequest{}, AlertRuleStateRequest{
		StartTime: 1, EndTime: 61, Step: 60,
	})
	if err == nil {
		t.Fatalf("Expected API error for closed port")
	}
}

func mapKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
