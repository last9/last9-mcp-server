package alerting

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestFormatRuleLifecycle(t *testing.T) {
	const quiet = "  Muted until: none\n  Recurring mute schedule: none\n"
	for name, tt := range map[string]struct {
		rule string
		want string
	}{
		"all omitted, active": {
			`{"state":"active"}`,
			"  Disabled: false\n" + quiet,
		},
		"is_disabled absent, state disabled": {
			`{"state":"disabled"}`,
			"  Disabled: true\n" + quiet,
		},
		"is_disabled absent, legacy state in other case and padded": {
			`{"state":" Disabled "}`,
			"  Disabled: true\n" + quiet,
		},
		"is_disabled true wins over state muted": {
			`{"state":"muted","is_disabled":true}`,
			"  Disabled: true\n" + quiet,
		},
		"is_disabled false wins over state disabled": {
			`{"state":"disabled","is_disabled":false}`,
			"  Disabled: false\n" + quiet,
		},
		"mute_until zero": {
			`{"state":"active","mute_until":0}`,
			"  Disabled: false\n" + quiet,
		},
		"mute_until -1": {
			`{"state":"muted","mute_until":-1}`,
			"  Disabled: false\n  Muted until: indefinitely\n  Recurring mute schedule: none\n",
		},
		"mute_until epoch": {
			`{"state":"muted","mute_until":1700000000}`,
			"  Disabled: false\n  Muted until: 2023-11-14T22:13:20Z\n  Recurring mute schedule: none\n",
		},
		"next_transition_at epoch": {
			`{"state":"muted","mute_until":1700000000,"next_transition_at":1700000000}`,
			"  Disabled: false\n  Muted until: 2023-11-14T22:13:20Z\n  Recurring mute schedule: none\n  Next transition: 2023-11-14T22:13:20Z\n",
		},
		"next_transition_at zero": {
			`{"state":"active","next_transition_at":0}`,
			"  Disabled: false\n" + quiet,
		},
		"schedule null": {
			`{"state":"active","recurring_mute_schedule":null}`,
			"  Disabled: false\n" + quiet,
		},
		"schedule multi-window": {
			`{"state":"active","recurring_mute_schedule":{"enabled":true,"timezone":"Asia/Kolkata","windows":[{"weekdays":["mon","tue"],"start_time":"22:00","end_time":"06:00"},{"weekdays":["sat","sun"],"start_time":"00:00","end_time":"23:59"}]}}`,
			"  Disabled: false\n  Muted until: none\n  Recurring mute schedule: timezone Asia/Kolkata\n    Mon,Tue 22:00-06:00\n    Sat,Sun 00:00-23:59\n",
		},
		"schedule legacy scalar": {
			`{"state":"active","recurring_mute_schedule":{"enabled":true,"timezone":"UTC","weekdays":["fri"],"start_time":"18:00","end_time":"09:00"}}`,
			"  Disabled: false\n  Muted until: none\n  Recurring mute schedule: timezone UTC\n    Fri 18:00-09:00\n",
		},
		"schedule disabled": {
			`{"state":"active","recurring_mute_schedule":{"enabled":false,"timezone":"UTC","windows":[{"weekdays":["wed"],"start_time":"01:00","end_time":"02:00"}]}}`,
			"  Disabled: false\n  Muted until: none\n  Recurring mute schedule: timezone UTC (enabled: false)\n    Wed 01:00-02:00\n",
		},
		"schedule blank weekdays are skipped": {
			`{"state":"active","recurring_mute_schedule":{"enabled":true,"timezone":"UTC","windows":[{"weekdays":["","sat"," ","sun"],"start_time":"01:00","end_time":"02:00"}]}}`,
			"  Disabled: false\n  Muted until: none\n  Recurring mute schedule: timezone UTC\n    Sat,Sun 01:00-02:00\n",
		},
		"schedule window with only blank weekdays": {
			`{"state":"active","recurring_mute_schedule":{"enabled":true,"timezone":"UTC","windows":[{"weekdays":[""],"start_time":"01:00","end_time":"02:00"}]}}`,
			"  Disabled: false\n  Muted until: none\n  Recurring mute schedule: timezone UTC\n    (no weekdays) 01:00-02:00\n",
		},
	} {
		t.Run(name, func(t *testing.T) {
			var rule AlertRule
			if err := json.Unmarshal([]byte(tt.rule), &rule); err != nil {
				t.Fatal(err)
			}
			if got := formatRuleLifecycle(rule); got != tt.want {
				t.Fatalf("got:\n%s\nwant:\n%s", got, tt.want)
			}
		})
	}
}

func TestRecurringMuteScheduleLegacyScalarsIgnoredWhenWindowsPresent(t *testing.T) {
	var s RecurringMuteSchedule
	raw := `{"enabled":true,"timezone":"UTC","windows":[{"weekdays":["mon"],"start_time":"01:00","end_time":"02:00"}],"weekdays":["fri"],"start_time":"18:00","end_time":"09:00"}`
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		t.Fatal(err)
	}
	if len(s.Windows) != 1 || s.Windows[0].Weekdays[0] != "mon" || !s.Enabled || s.Timezone != "UTC" {
		t.Fatalf("got %+v", s)
	}
}

func lifecycleTestRule(entityID string) AlertRule {
	disabled, next := true, int64(1700000000)
	return AlertRule{
		ID: "rule-1", EntityID: entityID, RuleName: "Paused", State: "disabled", Severity: "breach",
		IsDisabled: &disabled, MuteUntil: -1, NextTransitionAt: &next,
		RecurringMuteSchedule: &RecurringMuteSchedule{Enabled: true, Timezone: "UTC", Windows: []RecurringMuteWindow{{Weekdays: []string{"sun"}, StartTime: "00:00", EndTime: "06:00"}}},
	}
}

const lifecycleTestRuleText = "  Disabled: true\n  Muted until: indefinitely\n  Recurring mute schedule: timezone UTC\n    Sun 00:00-06:00\n  Next transition: 2023-11-14T22:13:20Z\n"

func TestGetEntityAlertRulesHandler_ShowsLifecycle(t *testing.T) {
	state := &entityAlertRulesTestServerState{
		rulesByEntityID: map[string]AlertConfigResponse{"entity-abc": {lifecycleTestRule("entity-abc")}},
	}
	text, _, err := executeGetEntityAlertRules(t, state, GetEntityAlertRulesArgs{EntityID: "entity-abc"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, lifecycleTestRuleText) {
		t.Fatalf("lifecycle block missing:\n%s", text)
	}
}

func TestGetAlertConfigHandler_ShowsLifecycle(t *testing.T) {
	state := alertConfigTestServerState{
		alertRules:         AlertConfigResponse{lifecycleTestRule("entity-abc")},
		alertRulesStatus:   http.StatusOK,
		entityLookupStatus: http.StatusOK,
	}
	text, _, err := executeGetAlertConfig(t, &state, GetAlertConfigArgs{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "  State: disabled\n"+lifecycleTestRuleText) {
		t.Fatalf("lifecycle block missing after State:\n%s", text)
	}
}
