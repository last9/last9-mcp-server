package alerting

import (
	"context"
	"io"
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
	assertAlertLinkMeta(t, result, expectedAlertLink{URL: "/v2/organizations//alerting/groups?group_id=entity-1&rule=rule-1", Label: "Open alert"})
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

func TestUpdateAlertCarriesOrClearsOnNoData(t *testing.T) {
	for _, tt := range []struct {
		name  string
		input *string
		want  string
	}{
		{name: "omitted carries stored value", want: `"on_no_data":"alert"`},
		{name: "empty clears with null", input: ptrString(""), want: `"on_no_data":null`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					_, _ = w.Write([]byte(`{"id":"rule-1","entity_id":"entity-1","on_no_data":"alert"}`))
					return
				}
				body, _ := io.ReadAll(r.Body)
				if !strings.Contains(string(body), tt.want) {
					t.Errorf("body = %s, want field %s", body, tt.want)
				}
				_, _ = w.Write([]byte(`{"id":"rule-2"}`))
			}))
			defer server.Close()
			payload := validAlertRulePayload(nil)
			payload.OnNoData = tt.input
			_, _, err := NewUpdateAlertHandler(server.Client(), testAlertMutationConfig(server.URL))(context.Background(), nil,
				UpdateAlertArgs{EntityID: "entity-1", ID: "rule-1", AlertRuleMutation: AlertRuleMutation{AlertRule: payload}})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}
