package alerting

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestDeleteAlertHandler(t *testing.T) {
	server := newAlertRuleServer(t, http.MethodDelete, "/entities/entity-1/alert-rules/rule-1", "")
	result, _, err := NewDeleteAlertHandler(server.Client(), testAlertMutationConfig(server.URL))(context.Background(), nil, DeleteAlertArgs{EntityID: "entity-1", ID: "rule-1"})
	if err != nil {
		t.Fatal(err)
	}
	if resultText(result) != `{"deleted":true,"id":"rule-1"}` {
		t.Fatalf("unexpected result: %s", resultText(result))
	}
}

func TestDeleteAlertValidationReturnsToolError(t *testing.T) {
	cfg := testAlertMutationConfig("http://unused")
	deleteResult, _, err := NewDeleteAlertHandler(http.DefaultClient, cfg)(context.Background(), nil, DeleteAlertArgs{})
	if err != nil || !deleteResult.IsError {
		t.Fatalf("delete validation: result=%+v err=%v", deleteResult, err)
	}
}

func TestDeleteAlertTransportEOFIsNotSuccess(t *testing.T) {
	result, _, err := NewDeleteAlertHandler(eofClient(), testAlertMutationConfig("http://unused.invalid"))(context.Background(), nil, DeleteAlertArgs{EntityID: "entity-1", ID: "rule-1"})
	if err == nil && (result == nil || !result.IsError) {
		t.Fatalf("transport EOF reported as success: %s", resultText(result))
	}
	if result != nil && strings.Contains(resultText(result), `"deleted":true`) {
		t.Fatalf("delete reported success: %s", resultText(result))
	}
}
