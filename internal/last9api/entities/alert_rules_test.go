package entities

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/last9api"
	"last9-mcp/internal/models"
)

func newTestClient(srv *httptest.Server) *last9api.Client {
	return last9api.NewClient(srv.Client(), models.Config{
		APIBaseURL:   srv.URL,
		TokenManager: &auth.TokenManager{AccessToken: "test-token", ExpiresAt: time.Now().Add(time.Hour)},
	})
}

func TestAlertRuleRequests(t *testing.T) {
	in := AlertRuleInput{EntityID: "entity/1", RuleID: "rule/1", Body: map[string]string{"k": "v"}}
	tests := []struct {
		name, method, path, body string
		call                     func(*last9api.Client) error
	}{
		{"create", http.MethodPost, "/entities/entity%2F1/alert-rules", `{"k":"v"}`, func(c *last9api.Client) error {
			_, err := CreateAlertRule(context.Background(), c, in)
			return err
		}},
		{"update", http.MethodPut, "/entities/entity%2F1/alert-rules/rule%2F1", `{"k":"v"}`, func(c *last9api.Client) error {
			_, err := UpdateAlertRule(context.Background(), c, in)
			return err
		}},
		{"patch", http.MethodPatch, "/entities/entity%2F1/alert-rules/rule%2F1", `{"k":"v"}`, func(c *last9api.Client) error {
			return PatchAlertRule(context.Background(), c, in)
		}},
		{"delete", http.MethodDelete, "/entities/entity%2F1/alert-rules/rule%2F1", ``, func(c *last9api.Client) error {
			_, err := DeleteAlertRule(context.Background(), c, in)
			return err
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != tt.method || r.URL.EscapedPath() != tt.path {
					t.Errorf("request = %s %s", r.Method, r.URL.EscapedPath())
				}
				if got, _ := io.ReadAll(r.Body); string(got) != tt.body {
					t.Errorf("body = %s", got)
				}
				w.WriteHeader(http.StatusNoContent)
			}))
			defer srv.Close()
			if err := tt.call(newTestClient(srv)); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestGetAlertRuleDecodesOwner(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.EscapedPath() != "/entities/entity%2F1/alert-rules/rule%2F1" {
			t.Errorf("request = %s %s", r.Method, r.URL.EscapedPath())
		}
		_, _ = w.Write([]byte(`{"id":"rule/1","entity_id":"entity-2","rule_name":"x","properties":{"annotations":{"team":"core"}}}`))
	}))
	defer srv.Close()
	rule, err := GetAlertRule(context.Background(), newTestClient(srv), AlertRuleInput{EntityID: "entity/1", RuleID: "rule/1"})
	if err != nil || rule.ID != "rule/1" || rule.EntityID != "entity-2" || rule.RuleName != "x" || rule.Properties.Annotations["team"] != "core" {
		t.Fatalf("rule=%+v err=%v", rule, err)
	}
}

func TestListAlertRulesDecodesRules(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.EscapedPath() != "/entities/entity%2F1/alert-rules" {
			t.Errorf("request = %s %s", r.Method, r.URL.EscapedPath())
		}
		_, _ = w.Write([]byte(`{"rules":[{"id":"rule-1","entity_id":"entity/1","rule_name":"latency"}]}`))
	}))
	defer srv.Close()
	rules, err := ListAlertRules(context.Background(), newTestClient(srv), "entity/1")
	if err != nil || len(rules) != 1 || rules[0].ID != "rule-1" || rules[0].EntityID != "entity/1" || rules[0].RuleName != "latency" {
		t.Fatalf("rules=%+v err=%v", rules, err)
	}
}

func TestListAlertRulesReturnsUpstreamError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	if _, err := ListAlertRules(context.Background(), newTestClient(srv), "e"); err == nil {
		t.Fatal("expected an error")
	}
}

func TestCreateAlertRuleReturnsRawBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"id":"rule-1"}`))
	}))
	defer srv.Close()
	raw, err := CreateAlertRule(context.Background(), newTestClient(srv), AlertRuleInput{EntityID: "e", Body: struct{}{}})
	if err != nil || string(raw) != `{"id":"rule-1"}` {
		t.Fatalf("raw=%s err=%v", raw, err)
	}
}
