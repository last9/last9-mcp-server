package alerting

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUpdateAlertHandler(t *testing.T) {
	server := newAlertRuleServer(t, http.MethodPut, "/entities/entity-1/alert-rules/rule%2F1",
		`{"primary_indicator":"latency","expression_args":{"latency":{"id":"kpi-1"}},"rule_name":"latency v2"}`)
	name := "latency v2"
	result, _, err := NewUpdateAlertHandler(server.Client(), testAlertMutationConfig(server.URL))(context.Background(), nil, UpdateAlertArgs{EntityID: "entity-1", ID: "rule/1", AlertRuleMutation: AlertRuleMutation{AlertRule: validAlertRulePayload(&name)}})
	if err != nil {
		t.Fatal(err)
	}
	if resultText(result) != `{"id":"rule-1"}` {
		t.Fatalf("unexpected result: %s", resultText(result))
	}
}

func TestUpdateAlertValidationReturnsToolError(t *testing.T) {
	cfg := testAlertMutationConfig("http://unused")
	update, _, err := NewUpdateAlertHandler(http.DefaultClient, cfg)(context.Background(), nil, UpdateAlertArgs{})
	if err != nil || !update.IsError {
		t.Fatalf("update validation: result=%+v err=%v", update, err)
	}
}

func TestUpdateAlertRejectsEmptyReplacementIDResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !serveRuleOwner(w, r) {
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer server.Close()
	_, _, err := NewUpdateAlertHandler(server.Client(), testAlertMutationConfig(server.URL))(context.Background(), nil, UpdateAlertArgs{
		EntityID: "entity-1", ID: "rule-1", AlertRuleMutation: AlertRuleMutation{AlertRule: validAlertRulePayload(nil)},
	})
	if err == nil || !strings.Contains(err.Error(), "replacement rule ID is unknown") {
		t.Fatalf("expected replacement ID error, got %v", err)
	}
}
