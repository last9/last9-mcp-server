package alerting

import (
	"strings"
	"testing"
)

const (
	sampleKPIID  = "kpi-uuid-1"
	samplePromQL = `histogram_quantile(0.99, rate(http_request_duration_seconds_bucket[5m]))`
)

func sampleConfiguredAndUnconfiguredKPIRules() AlertConfigResponse {
	return AlertConfigResponse{
		{
			ID:               "rule-1",
			OrganizationID:   "org-1",
			EntityID:         "entity-1",
			PrimaryIndicator: "p99_latency",
			Expression:       "p99_latency",
			State:            "active",
			Severity:         "breach",
			Algorithm:        "static_threshold",
			RuleName:         "High P99",
			ExpressionArgs: map[string]AlertRuleExpressionArg{
				"p99_latency": {ID: sampleKPIID},
			},
		},
		{
			ID:               "rule-2",
			OrganizationID:   "org-1",
			EntityID:         "entity-2",
			PrimaryIndicator: "error_rate",
			Expression:       "errors_total",
			State:            "active",
			Severity:         "breach",
			Algorithm:        "static_threshold",
			RuleName:         "Errors",
			ExpressionArgs: map[string]AlertRuleExpressionArg{
				"errors_total": {ID: sampleKPIID},
			},
		},
	}
}

func sampleUnconfiguredKPIRule() AlertConfigResponse {
	rules := sampleConfiguredAndUnconfiguredKPIRules()
	return AlertConfigResponse{rules[1]}
}

func alertConfigKPIResolutionState(rules AlertConfigResponse) alertConfigTestServerState {
	return alertConfigTestServerState{
		alertRules:   rules,
		entityGroups: sampleAlertGroupEntities(),
		notificationChannels: []NotificationChannel{
			{ID: 1, Name: "Checkout Slack", Type: "slack", Severity: "breach", ServiceFQID: "entity-1"},
		},
		kpiResponses: map[string]kpiResponse{
			sampleKPIID: {
				ID:   sampleKPIID,
				Name: "p99_latency",
				Definition: kpiDefinition{
					Query: samplePromQL,
					Unit:  "seconds",
				},
			},
		},
	}
}

func TestGetAlertConfigHandler_NotificationChannelTypeAndUnconfiguredOR(t *testing.T) {
	state := alertConfigTestServerState{
		alertRules:         sampleAlertConfigRules(),
		entityGroups:       sampleAlertGroupEntities(),
		notificationChannels: []NotificationChannel{
			{ID: 1, Name: "Checkout Slack", Type: "slack", ServiceFQID: "entity-1"},
		},
	}

	text, _, err := executeGetAlertConfig(t, &state, GetAlertConfigArgs{
		OnlyWithoutNotificationChannel: true,
		NotificationChannelTypes:       []string{"slack"},
	})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if !strings.Contains(text, "ID: rule-1") || !strings.Contains(text, "ID: rule-2") || !strings.Contains(text, "ID: rule-3") {
		t.Fatalf("expected rule-1 (slack) and unconfigured rule-2/rule-3, got:\n%s", text)
	}
	if strings.Contains(text, "no per-entity notification channel configured") {
		t.Fatalf("OR filter must use default header, not unconfigured-only header, got:\n%s", text)
	}
	if !strings.Contains(text, "Found 3 alert rules:") {
		t.Fatalf("expected default count header for OR filter, got:\n%s", text)
	}
	if strings.Contains(text, "Global notification channels:") {
		t.Fatalf("OR-combined filter must not prepend global-channel advisory when configured rules are present, got:\n%s", text)
	}
}

// TestGetAlertConfigHandler_NotificationChannelAndUnconfiguredOR_ResolvesKPIs
// guards the bug where OR-combining only_without_notification_channel with a
// notification_channel_* filter caused the handler to skip KPI resolution for
// the entire result set and prepend the global-channel advisory. The table
// covers all three notification_channel_* axes plus the pure unconfigured-only
// path (isUnconfiguredOnlyRequest true with no channel filters).
func TestGetAlertConfigHandler_NotificationChannelAndUnconfiguredOR_ResolvesKPIs(t *testing.T) {
	cases := []struct {
		name       string
		channelArgs GetAlertConfigArgs
		orArgs     GetAlertConfigArgs
		pureOnly   bool
	}{
		{
			name:        "types",
			channelArgs: GetAlertConfigArgs{NotificationChannelTypes: []string{"slack"}},
			orArgs:      GetAlertConfigArgs{OnlyWithoutNotificationChannel: true, NotificationChannelTypes: []string{"slack"}},
		},
		{
			name:        "names",
			channelArgs: GetAlertConfigArgs{NotificationChannelNames: []string{"Checkout Slack"}},
			orArgs:      GetAlertConfigArgs{OnlyWithoutNotificationChannel: true, NotificationChannelNames: []string{"Checkout Slack"}},
		},
		{
			name:        "severities",
			channelArgs: GetAlertConfigArgs{NotificationChannelSeverities: []string{"breach"}},
			orArgs:      GetAlertConfigArgs{OnlyWithoutNotificationChannel: true, NotificationChannelSeverities: []string{"breach"}},
		},
		{
			name:     "pure unconfigured-only",
			pureOnly: true,
			orArgs:   GetAlertConfigArgs{OnlyWithoutNotificationChannel: true},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.pureOnly {
				state := alertConfigKPIResolutionState(sampleUnconfiguredKPIRule())
				text, _, err := executeGetAlertConfig(t, &state, tc.orArgs)
				if err != nil {
					t.Fatalf("handler returned error: %v", err)
				}
				if !strings.Contains(text, "ID: rule-2") {
					t.Fatalf("expected unconfigured rule-2, got:\n%s", text)
				}
				if !strings.Contains(text, "no per-entity notification channel configured") {
					t.Fatalf("expected unconfigured-only header, got:\n%s", text)
				}
				if !strings.Contains(text, "Global notification channels:") {
					t.Fatalf("expected global-channel advisory prepended for unconfigured-only, got:\n%s", text)
				}
				if strings.Contains(text, "PromQL:") || strings.Contains(text, "Unit:") {
					t.Fatalf("unconfigured-only must skip KPI resolution, got:\n%s", text)
				}
				return
			}

			channelState := alertConfigKPIResolutionState(sampleConfiguredAndUnconfiguredKPIRules())
			only, _, err := executeGetAlertConfig(t, &channelState, tc.channelArgs)
			if err != nil {
				t.Fatalf("channel-only: %v", err)
			}
			if !strings.Contains(only, "PromQL: "+samplePromQL) {
				t.Fatalf("channel-only: expected PromQL resolved for rule-1, got:\n%s", only)
			}
			if !strings.Contains(only, "Unit: seconds") {
				t.Fatalf("channel-only: expected Unit resolved for rule-1, got:\n%s", only)
			}
			if !strings.Contains(only, "ID: rule-1") {
				t.Fatalf("channel-only: expected rule-1 present, got:\n%s", only)
			}
			if strings.Contains(only, "Global notification channels:") {
				t.Fatalf("channel-only: expected no global-channel advisory, got:\n%s", only)
			}

			orState := alertConfigKPIResolutionState(sampleConfiguredAndUnconfiguredKPIRules())
			or, _, err := executeGetAlertConfig(t, &orState, tc.orArgs)
			if err != nil {
				t.Fatalf("OR-combined: %v", err)
			}
			if !strings.Contains(or, "ID: rule-1") || !strings.Contains(or, "ID: rule-2") {
				t.Fatalf("OR-combined: expected rule-1 and rule-2, got:\n%s", or)
			}
			if !strings.Contains(or, "PromQL: "+samplePromQL) {
				t.Fatalf("OR-combined: rule-1 lost PromQL under OR-combined filter; got:\n%s", or)
			}
			if !strings.Contains(or, "Unit: seconds") {
				t.Fatalf("OR-combined: rule-1 lost Unit under OR-combined filter; got:\n%s", or)
			}
			if strings.Contains(or, "Global notification channels:") {
				t.Fatalf("OR-combined: global-channel advisory prepended; got:\n%s", or)
			}
			if strings.Contains(or, "no per-entity notification channel configured") {
				t.Fatalf("OR-combined: must use default count header, got:\n%s", or)
			}
		})
	}
}
