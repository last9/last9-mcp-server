package alerting

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestAlertMutationHandlers(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		invoke     func(*http.Client, models.Config) error
		wantResult string
	}{
		{
			name: "create", method: http.MethodPost, path: "/entities/entity-1/alert-rules", body: `{"primary_indicator":"latency","expression_args":{"latency":{"id":"kpi-1"}},"rule_name":"latency"}`,
			invoke: func(client *http.Client, cfg models.Config) error {
				name := "latency"
				result, _, err := NewCreateAlertHandler(client, cfg)(context.Background(), nil, CreateAlertArgs{EntityID: "entity-1", AlertRuleMutation: AlertRuleMutation{AlertRule: validAlertRulePayload(&name)}})
				if err == nil && resultText(result) != `{"id":"rule-1"}` {
					t.Fatalf("unexpected result: %s", resultText(result))
				}
				return err
			},
		},
		{
			name: "update", method: http.MethodPut, path: "/entities/entity-1/alert-rules/rule%2F1", body: `{"primary_indicator":"latency","expression_args":{"latency":{"id":"kpi-1"}},"rule_name":"latency v2"}`,
			invoke: func(client *http.Client, cfg models.Config) error {
				name := "latency v2"
				result, _, err := NewUpdateAlertHandler(client, cfg)(context.Background(), nil, UpdateAlertArgs{EntityID: "entity-1", ID: "rule/1", AlertRuleMutation: AlertRuleMutation{AlertRule: validAlertRulePayload(&name)}})
				if err == nil && resultText(result) != `{"id":"rule-1"}` {
					t.Fatalf("unexpected result: %s", resultText(result))
				}
				return err
			},
		},
		{
			name: "patch", method: http.MethodPatch, path: "/entities/entity-1/alert-rules/rule-1", body: `{"is_disabled":false}`,
			invoke: func(client *http.Client, cfg models.Config) error {
				enabled := false
				result, _, err := NewPatchAlertHandler(client, cfg)(context.Background(), nil, PatchAlertArgs{EntityID: "entity-1", ID: "rule-1", IsDisabled: &enabled})
				if err == nil && resultText(result) != `{"patched":true,"id":"rule-1"}` {
					t.Fatalf("unexpected result: %s", resultText(result))
				}
				return err
			},
		},
		{
			name: "delete", method: http.MethodDelete, path: "/entities/entity-1/alert-rules/rule-1",
			invoke: func(client *http.Client, cfg models.Config) error {
				result, _, err := NewDeleteAlertHandler(client, cfg)(context.Background(), nil, DeleteAlertArgs{EntityID: "entity-1", ID: "rule-1"})
				if err == nil && resultText(result) != `{"deleted":true,"id":"rule-1"}` {
					t.Fatalf("unexpected result: %s", resultText(result))
				}
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != tt.method {
					t.Errorf("method = %s", r.Method)
				}
				if r.URL.EscapedPath() != tt.path {
					t.Errorf("path = %s", r.URL.EscapedPath())
				}
				if got := r.Header.Get("X-LAST9-API-TOKEN"); got == "" {
					t.Error("missing API token")
				}
				body, _ := io.ReadAll(r.Body)
				if string(body) != tt.body {
					t.Errorf("body = %s", body)
				}
				if tt.method == http.MethodPost || tt.method == http.MethodPut {
					_, _ = w.Write([]byte(`{"id":"rule-1"}`))
				}
			}))
			defer server.Close()
			cfg := testAlertMutationConfig(server.URL)
			if err := tt.invoke(server.Client(), cfg); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestAlertMutationValidationReturnsToolErrors(t *testing.T) {
	cfg := testAlertMutationConfig("http://unused")
	create, _, err := NewCreateAlertHandler(http.DefaultClient, cfg)(context.Background(), nil, CreateAlertArgs{})
	if err != nil || !create.IsError {
		t.Fatalf("create validation: result=%+v err=%v", create, err)
	}
	update, _, err := NewUpdateAlertHandler(http.DefaultClient, cfg)(context.Background(), nil, UpdateAlertArgs{})
	if err != nil || !update.IsError {
		t.Fatalf("update validation: result=%+v err=%v", update, err)
	}
	deleteResult, _, err := NewDeleteAlertHandler(http.DefaultClient, cfg)(context.Background(), nil, DeleteAlertArgs{})
	if err != nil || !deleteResult.IsError {
		t.Fatalf("delete validation: result=%+v err=%v", deleteResult, err)
	}
	patchResult, _, err := NewPatchAlertHandler(http.DefaultClient, cfg)(context.Background(), nil, PatchAlertArgs{})
	if err != nil || !patchResult.IsError {
		t.Fatalf("patch validation: result=%+v err=%v", patchResult, err)
	}
}

func TestAlertMutationAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "invalid alert rule", http.StatusUnprocessableEntity)
	}))
	defer server.Close()
	_, _, err := NewCreateAlertHandler(server.Client(), testAlertMutationConfig(server.URL))(context.Background(), nil, CreateAlertArgs{EntityID: "entity-1", AlertRuleMutation: AlertRuleMutation{AlertRule: validAlertRulePayload(nil)}})
	if err == nil {
		t.Fatal("expected API error")
	}
}

func TestRecommendAlertConfig(t *testing.T) {
	result, _, err := NewRecommendAlertConfigHandler(nil, models.Config{})(context.Background(), nil, RecommendAlertConfigArgs{
		Signal:    "request error ratio (%)",
		Objective: "detect sustained checkout failures",
		RuleType:  "static",
	})
	if err != nil {
		t.Fatal(err)
	}
	text := resultText(result)
	for _, want := range []string{"static", "is_disabled=true", "runbook", "annotations", "patch_alert"} {
		if !strings.Contains(text, want) {
			t.Errorf("recommendation missing %q", want)
		}
	}
}

func TestUpdateAlertRejectsEmptyReplacementIDResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	defer server.Close()
	_, _, err := NewUpdateAlertHandler(server.Client(), testAlertMutationConfig(server.URL))(context.Background(), nil, UpdateAlertArgs{
		EntityID: "entity-1", ID: "rule-1", AlertRuleMutation: AlertRuleMutation{AlertRule: validAlertRulePayload(nil)},
	})
	if err == nil || !strings.Contains(err.Error(), "replacement rule ID is unknown") {
		t.Fatalf("expected replacement ID error, got %v", err)
	}
}

func TestAdaptiveAlertRuleType(t *testing.T) {
	if err := validateGetAlertConfigArgs(GetAlertConfigArgs{RuleType: "adaptive"}); err != nil {
		t.Fatal(err)
	}
	if got := alertConfigRuleType(AlertRule{Algorithm: "adaptive-threshold-v1"}); got != "adaptive" {
		t.Fatalf("rule type = %q", got)
	}
}

func resultText(result *mcp.CallToolResult) string {
	if result == nil || len(result.Content) == 0 {
		return ""
	}
	text, _ := result.Content[0].(*mcp.TextContent)
	if text == nil {
		return ""
	}
	return text.Text
}

func testAlertMutationConfig(baseURL string) models.Config {
	return models.Config{APIBaseURL: baseURL, TokenManager: &auth.TokenManager{AccessToken: "test-token", ExpiresAt: time.Now().Add(time.Hour)}}
}

func validAlertRulePayload(name *string) AlertRulePayload {
	return AlertRulePayload{
		PrimaryIndicator: "latency",
		ExpressionArgs:   map[string]AlertExpressionArg{"latency": {ID: "kpi-1"}},
		RuleName:         name,
	}
}
