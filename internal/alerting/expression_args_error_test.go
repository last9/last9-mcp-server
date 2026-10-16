package alerting

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The API accepts an expression_args key only when it equals the bound KPI's own name,
// while validate_alert_expression accepts any key, so the raw 400 reads as a dead end.
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
		if got.err != nil || !got.isErr || !strings.Contains(got.text, "exact name") || !strings.Contains(got.text, "nothing was saved") {
			t.Errorf("%s: text=%s isErr=%v err=%v", tool, got.text, got.isErr, got.err)
		}
	}
}

func TestAlertRuleWritesRelayOther400s(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"condition required"}`))
	}))
	defer server.Close()
	_, _, err := NewCreateAlertHandler(server.Client(), testAlertMutationConfig(server.URL))(context.Background(), nil, CreateAlertArgs{EntityID: "entity-1", AlertRuleMutation: AlertRuleMutation{AlertRule: validAlertRulePayload(nil)}})
	if err == nil || !strings.Contains(err.Error(), "condition required") {
		t.Fatalf("err = %v", err)
	}
}
