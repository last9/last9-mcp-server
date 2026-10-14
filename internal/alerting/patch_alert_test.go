package alerting

import (
	"context"
	"net/http"
	"testing"
)

func TestPatchAlertHandler(t *testing.T) {
	server := newAlertRuleServer(t, http.MethodPatch, "/entities/entity-1/alert-rules/rule-1", `{"is_disabled":false}`)
	enabled := false
	result, _, err := NewPatchAlertHandler(server.Client(), testAlertMutationConfig(server.URL))(context.Background(), nil, PatchAlertArgs{EntityID: "entity-1", ID: "rule-1", IsDisabled: &enabled})
	if err != nil {
		t.Fatal(err)
	}
	if resultText(result) != `{"patched":true,"id":"rule-1"}` {
		t.Fatalf("unexpected result: %s", resultText(result))
	}
}

func TestPatchAlertValidationReturnsToolError(t *testing.T) {
	cfg := testAlertMutationConfig("http://unused")
	patchResult, _, err := NewPatchAlertHandler(http.DefaultClient, cfg)(context.Background(), nil, PatchAlertArgs{})
	if err != nil || !patchResult.IsError {
		t.Fatalf("patch validation: result=%+v err=%v", patchResult, err)
	}
}
