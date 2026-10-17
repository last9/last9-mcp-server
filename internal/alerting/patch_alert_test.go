package alerting

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestPatchAlertSendsRecurringMuteSchedule(t *testing.T) {
	server := newAlertRuleServer(t, http.MethodPatch, "/entities/entity-1/alert-rules/rule-1",
		`{"recurring_mute_schedule":{"enabled":true,"timezone":"Asia/Kolkata","windows":[{"weekdays":["sat","sun"],"start_time":"22:00","end_time":"06:00"}]}}`)
	result, _, err := NewPatchAlertHandler(server.Client(), testAlertMutationConfig(server.URL))(context.Background(), nil, PatchAlertArgs{
		EntityID: "entity-1", ID: "rule-1",
		RecurringMuteSchedule: muteSchedule("Asia/Kolkata", RecurringMuteWindow{Weekdays: []string{"SAT", "sun", "sat"}, StartTime: "22:00", EndTime: "06:00"}),
	})
	if err != nil || result.IsError {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestPatchAlertClearsRecurringMuteSchedule(t *testing.T) {
	server := newAlertRuleServer(t, http.MethodPatch, "/entities/entity-1/alert-rules/rule-1", `{"recurring_mute_schedule":null}`)
	result, _, err := NewPatchAlertHandler(server.Client(), testAlertMutationConfig(server.URL))(context.Background(), nil, PatchAlertArgs{
		EntityID: "entity-1", ID: "rule-1", ClearRecurringMuteSchedule: true,
	})
	if err != nil || result.IsError {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestPatchAlertOmitsScheduleKeyWhenNotCleared(t *testing.T) {
	server := newAlertRuleServer(t, http.MethodPatch, "/entities/entity-1/alert-rules/rule-1", `{"mute_until":0}`)
	zero := int64(0)
	result, _, err := NewPatchAlertHandler(server.Client(), testAlertMutationConfig(server.URL))(context.Background(), nil, PatchAlertArgs{
		EntityID: "entity-1", ID: "rule-1", MuteUntil: &zero,
	})
	if err != nil || result.IsError {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestPatchAlertRejectsClearWithSchedule(t *testing.T) {
	result, _, err := NewPatchAlertHandler(http.DefaultClient, testAlertMutationConfig("http://unused"))(context.Background(), nil, PatchAlertArgs{
		EntityID: "entity-1", ID: "rule-1", ClearRecurringMuteSchedule: true,
		RecurringMuteSchedule: muteSchedule("Asia/Kolkata", RecurringMuteWindow{Weekdays: []string{"sat"}, StartTime: "22:00", EndTime: "06:00"}),
	})
	if err != nil || !result.IsError || !strings.Contains(resultText(result), "cannot be combined") {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestPatchAlertRejectsInvalidScheduleBeforeRequest(t *testing.T) {
	result, _, err := NewPatchAlertHandler(http.DefaultClient, testAlertMutationConfig("http://unused"))(context.Background(), nil, PatchAlertArgs{
		EntityID: "entity-1", ID: "rule-1", RecurringMuteSchedule: muteSchedule("Nowhere/Land"),
	})
	if err != nil || !result.IsError {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestPatchAlertForbiddenExplainsViewerRole(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serveRuleOwner(w, r) {
			return
		}
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"Not allowed for viewer role"}`))
	}))
	defer server.Close()
	enabled := true
	result, _, err := NewPatchAlertHandler(server.Client(), testAlertMutationConfig(server.URL))(context.Background(), nil, PatchAlertArgs{EntityID: "entity-1", ID: "rule-1", IsDisabled: &enabled})
	if err != nil || !result.IsError {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if text := resultText(result); !strings.Contains(text, "Viewer-role") || !strings.Contains(text, "editor access") {
		t.Fatalf("unexpected message: %s", text)
	}
}
