package alerting

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCreateAlertHandler(t *testing.T) {
	server := newAlertRuleServer(t, http.MethodPost, "/entities/entity-1/alert-rules",
		`{"primary_indicator":"latency","expression_args":{"latency":{"id":"kpi-1"}},"rule_name":"latency"}`)
	name := "latency"
	result, _, err := NewCreateAlertHandler(server.Client(), testAlertMutationConfig(server.URL))(context.Background(), nil, CreateAlertArgs{EntityID: "entity-1", AlertRuleMutation: AlertRuleMutation{AlertRule: validAlertRulePayload(&name)}})
	if err != nil {
		t.Fatal(err)
	}
	if resultText(result) != `{"id":"rule-1"}` {
		t.Fatalf("unexpected result: %s", resultText(result))
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
