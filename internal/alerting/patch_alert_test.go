package alerting

import (
	"context"
	"io"
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

func newPatchPropertiesServer(t *testing.T, storedRule string, patched *string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		switch r.Method {
		case http.MethodGet:
			_, _ = w.Write([]byte(storedRule))
		case http.MethodPatch:
			*patched = string(body)
		default:
			t.Errorf("unexpected %s", r.Method)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func TestPatchAlertPropertiesKeepsOmittedStoredFields(t *testing.T) {
	stored := `{"id":"rule-1","entity_id":"entity-1","properties":{"description":"old","runbook":{"link":"https://runbook"},"annotations":{"team":"core"},"query_mode":"code"}}`
	tests := []struct {
		name  string
		patch AlertProperties
		want  string
	}{
		{"description only", AlertProperties{Description: "new"},
			`{"properties":{"description":"new","runbook":{"link":"https://runbook"},"annotations":{"team":"core"},"query_mode":"code"}}`},
		{"blank description keeps stored", AlertProperties{Description: "  ", QueryMode: "builder"},
			`{"properties":{"description":"old","runbook":{"link":"https://runbook"},"annotations":{"team":"core"},"query_mode":"builder"}}`},
		{"annotations replace the whole map", AlertProperties{Annotations: map[string]string{"owner": "sre"}},
			`{"properties":{"description":"old","runbook":{"link":"https://runbook"},"annotations":{"owner":"sre"},"query_mode":"code"}}`},
		{"empty annotations clear them", AlertProperties{Annotations: map[string]string{}},
			`{"properties":{"description":"old","runbook":{"link":"https://runbook"},"query_mode":"code"}}`},
		{"runbook only", AlertProperties{Runbook: &AlertRunbook{Link: "https://new"}},
			`{"properties":{"description":"old","runbook":{"link":"https://new"},"annotations":{"team":"core"},"query_mode":"code"}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var patched string
			server := newPatchPropertiesServer(t, stored, &patched)
			props := tt.patch
			result, _, err := NewPatchAlertHandler(server.Client(), testAlertMutationConfig(server.URL))(context.Background(), nil, PatchAlertArgs{EntityID: "entity-1", ID: "rule-1", Properties: &props})
			if err != nil || result.IsError {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			if patched != tt.want {
				t.Fatalf("patched body = %s\nwant %s", patched, tt.want)
			}
		})
	}
}

func TestPatchAlertPropertiesWithoutStoredProperties(t *testing.T) {
	for _, stored := range []string{
		`{"id":"rule-1","entity_id":"entity-1"}`,
		`{"id":"rule-1","entity_id":"entity-1","properties":null}`,
		`{"id":"rule-1","entity_id":"entity-1","properties":{}}`,
	} {
		var patched string
		server := newPatchPropertiesServer(t, stored, &patched)
		result, _, err := NewPatchAlertHandler(server.Client(), testAlertMutationConfig(server.URL))(context.Background(), nil, PatchAlertArgs{EntityID: "entity-1", ID: "rule-1", Properties: &AlertProperties{Description: "new"}})
		if err != nil || result.IsError {
			t.Fatalf("stored=%s result=%+v err=%v", stored, result, err)
		}
		if patched != `{"properties":{"description":"new"}}` {
			t.Fatalf("stored=%s patched body = %s", stored, patched)
		}
	}
}

func TestPatchAlertPropertiesRefusesUnreadableStoredProperties(t *testing.T) {
	patched := "<not sent>"
	server := newPatchPropertiesServer(t, `{"id":"rule-1","entity_id":"entity-1","properties":"oops"}`, &patched)
	result, _, err := NewPatchAlertHandler(server.Client(), testAlertMutationConfig(server.URL))(context.Background(), nil, PatchAlertArgs{EntityID: "entity-1", ID: "rule-1", Properties: &AlertProperties{Description: "new"}})
	if err != nil || !result.IsError || !strings.Contains(resultText(result), "nothing was changed") {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if patched != "<not sent>" {
		t.Fatalf("PATCH sent despite unreadable stored properties: %s", patched)
	}
}

func TestPatchAlertWithoutPropertiesIgnoresStoredProperties(t *testing.T) {
	var patched string
	server := newPatchPropertiesServer(t, `{"id":"rule-1","entity_id":"entity-1","properties":"oops"}`, &patched)
	enabled := false
	result, _, err := NewPatchAlertHandler(server.Client(), testAlertMutationConfig(server.URL))(context.Background(), nil, PatchAlertArgs{EntityID: "entity-1", ID: "rule-1", IsDisabled: &enabled})
	if err != nil || result.IsError || patched != `{"is_disabled":false}` {
		t.Fatalf("result=%+v err=%v patched=%s", result, err, patched)
	}
}
