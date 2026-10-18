package alerting

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The API accepts an expression_args key only when it equals the bound KPI's own name and the
// KPI belongs to the group, and answers both mistakes with one bare 400.
func TestAlertRuleWritesExplainInvalidExpressionArgs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serveRuleOwner(w, r) {
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"Invalid expression_args"}`))
	}))
	defer server.Close()
	cfg := testAlertMutationConfig(server.URL)
	ctx := context.Background()
	rule := AlertRuleMutation{AlertRule: validAlertRulePayload(nil)}
	create, _, createErr := NewCreateAlertHandler(server.Client(), cfg)(ctx, nil, CreateAlertArgs{EntityID: "entity-1", AlertRuleMutation: rule})
	update, _, updateErr := NewUpdateAlertHandler(server.Client(), cfg)(ctx, nil, UpdateAlertArgs{EntityID: "entity-1", ID: "rule-1", AlertRuleMutation: rule})
	for tool, got := range map[string]struct {
		text  string
		isErr bool
		err   error
	}{
		"create_alert": {resultText(create), create != nil && create.IsError, createErr},
		"update_alert": {resultText(update), update != nil && update.IsError, updateErr},
	} {
		if got.err != nil || !got.isErr || !strings.Contains(got.text, "exact name") || !strings.Contains(got.text, "nothing was saved") ||
			!strings.Contains(got.text, "a KPI id is not a KPI of entity_id") || !strings.Contains(got.text, "invalid indicator details") {
			t.Errorf("%s: text=%s isErr=%v err=%v", tool, got.text, got.isErr, got.err)
		}
	}
}

// The API requires condition, alert_condition and eval_window only on static rules, which it
// tells apart from adaptive ones itself, so the tool maps its answer instead of guessing.
func TestAlertRuleWritesExplainMissingStaticField(t *testing.T) {
	for upstream, field := range map[string]string{
		"condition required":       "alert_rule.condition",
		"alert condition required": "alert_rule.alert_condition",
		"eval window required":     "alert_rule.eval_window",
	} {
		t.Run(field, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if serveRuleOwner(w, r) {
					return
				}
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"` + upstream + `"}`))
			}))
			defer server.Close()
			cfg := testAlertMutationConfig(server.URL)
			ctx := context.Background()
			rule := AlertRuleMutation{AlertRule: validAlertRulePayload(nil)}
			create, _, createErr := NewCreateAlertHandler(server.Client(), cfg)(ctx, nil, CreateAlertArgs{EntityID: "entity-1", AlertRuleMutation: rule})
			update, _, updateErr := NewUpdateAlertHandler(server.Client(), cfg)(ctx, nil, UpdateAlertArgs{EntityID: "entity-1", ID: "rule-1", AlertRuleMutation: rule})
			for tool, got := range map[string]struct {
				text  string
				isErr bool
				err   error
			}{
				"create_alert": {resultText(create), create != nil && create.IsError, createErr},
				"update_alert": {resultText(update), update != nil && update.IsError, updateErr},
			} {
				want := field + " is required for a static rule (an expression without adaptive_std_cmp); nothing was saved"
				if got.err != nil || !got.isErr || !strings.HasPrefix(got.text, want) {
					t.Errorf("%s: text=%s isErr=%v err=%v", tool, got.text, got.isErr, got.err)
				}
			}
		})
	}
}

func TestAlertRuleWritesRelayOther400s(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"condition cannot be empty"}`))
	}))
	defer server.Close()
	_, _, err := NewCreateAlertHandler(server.Client(), testAlertMutationConfig(server.URL))(context.Background(), nil, CreateAlertArgs{EntityID: "entity-1", AlertRuleMutation: AlertRuleMutation{AlertRule: validAlertRulePayload(nil)}})
	if err == nil || !strings.Contains(err.Error(), "condition cannot be empty") {
		t.Fatalf("err = %v", err)
	}
}
