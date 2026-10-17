package alerting

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCreateAlertAlwaysCreatesDisabledDraft(t *testing.T) {
	f, tr := false, true
	for name, in := range map[string]*bool{"omitted": nil, "false": &f, "true": &tr} {
		t.Run(name, func(t *testing.T) {
			server := newAlertRuleServer(t, http.MethodPost, "/entities/entity-1/alert-rules",
				`{"primary_indicator":"latency","expression_args":{"latency":{"id":"kpi-1"}},"is_disabled":true,"rule_name":"latency"}`)
			rn := "latency"
			payload := validAlertRulePayload(&rn)
			payload.IsDisabled = in
			result, _, err := NewCreateAlertHandler(server.Client(), testAlertMutationConfig(server.URL))(context.Background(), nil, CreateAlertArgs{EntityID: "entity-1", AlertRuleMutation: AlertRuleMutation{AlertRule: payload}})
			if err != nil {
				t.Fatal(err)
			}
			text := resultText(result)
			if !strings.HasPrefix(text, `{"id":"rule-1"}`) || !strings.Contains(text, "disabled draft") || !strings.Contains(text, "patch_alert is_disabled=false") {
				t.Fatalf("unexpected result: %s", text)
			}
			if got := strings.Contains(text, "was ignored"); got != (name == "false") {
				t.Fatalf("override note = %v for %s: %s", got, name, text)
			}
		})
	}
}

func TestCreateAlertValidationReturnsToolError(t *testing.T) {
	cfg := testAlertMutationConfig("http://unused")
	create, _, err := NewCreateAlertHandler(http.DefaultClient, cfg)(context.Background(), nil, CreateAlertArgs{})
	if err != nil || !create.IsError {
		t.Fatalf("create validation: result=%+v err=%v", create, err)
	}
}

func TestCreateAlertAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "invalid alert rule", http.StatusUnprocessableEntity)
	}))
	defer server.Close()
	_, _, err := NewCreateAlertHandler(server.Client(), testAlertMutationConfig(server.URL))(context.Background(), nil, CreateAlertArgs{EntityID: "entity-1", AlertRuleMutation: AlertRuleMutation{AlertRule: validAlertRulePayload(nil)}})
	if err == nil {
		t.Fatal("expected API error")
	}
}

func TestCreateAlertRefusesNameAlreadyInGroup(t *testing.T) {
	var posted bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.EscapedPath() == "/entities/entity-1/alert-rules" {
			_, _ = w.Write([]byte(`{"rules":[{"id":"rule-8","entity_id":"entity-1","rule_name":"errors"},{"id":"rule-9","entity_id":"entity-1","rule_name":"Latency"}]}`))
			return
		}
		posted = true
		_, _ = w.Write([]byte(`{"id":"rule-1"}`))
	}))
	defer server.Close()
	name := " latency "
	result, _, err := NewCreateAlertHandler(server.Client(), testAlertMutationConfig(server.URL))(context.Background(), nil,
		CreateAlertArgs{EntityID: "entity-1", AlertRuleMutation: AlertRuleMutation{AlertRule: validAlertRulePayload(&name)}})
	if err != nil || !result.IsError {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	text := resultText(result)
	for _, want := range []string{"rule-9", "update_alert", "nothing was created"} {
		if !strings.Contains(text, want) {
			t.Errorf("text missing %q: %s", want, text)
		}
	}
	if posted {
		t.Error("create was sent despite the duplicate name")
	}
}

func TestCreateAlertFailsWhenExistingNamesCannotBeListed(t *testing.T) {
	var posted bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		posted = true
	}))
	defer server.Close()
	name := "latency"
	_, _, err := NewCreateAlertHandler(server.Client(), testAlertMutationConfig(server.URL))(context.Background(), nil,
		CreateAlertArgs{EntityID: "entity-1", AlertRuleMutation: AlertRuleMutation{AlertRule: validAlertRulePayload(&name)}})
	if err == nil || posted {
		t.Fatalf("err=%v posted=%v", err, posted)
	}
}
