package alerting

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type entityAlertRulesTestServerState struct {
	rulesByEntityID map[string]AlertConfigResponse // entityID → rules
	kpiResponses    map[string]kpiResponse          // kpiID → response (absent = 404)
	entityStatus    int                             // default 200
}

func newEntityAlertRulesTestServer(t *testing.T, state *entityAlertRulesTestServerState) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// /entities/{entityID}/alert-rules
		if strings.HasPrefix(r.URL.Path, "/entities/") && strings.HasSuffix(r.URL.Path, "/alert-rules") {
			parts := strings.Split(r.URL.Path, "/")
			// ["", "entities", entityID, "alert-rules"]
			if len(parts) == 4 {
				entityID := parts[2]
				status := state.entityStatus
				if status == 0 {
					status = http.StatusOK
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				if status == http.StatusOK {
					rules := state.rulesByEntityID[entityID]
					_ = json.NewEncoder(w).Encode(struct {
						Rules        AlertConfigResponse `json:"rules"`
						SnoozedUntil int                 `json:"snoozed_until"`
					}{Rules: rules})
					return
				}
				_, _ = w.Write([]byte(`{"error":"entity alert rules failed"}`))
				return
			}
		}

		// /entities/{entityID}/kpis/{kpiID}
		if strings.HasPrefix(r.URL.Path, "/entities/") && strings.Contains(r.URL.Path, "/kpis/") {
			parts := strings.Split(r.URL.Path, "/")
			// ["", "entities", entityID, "kpis", kpiID]
			if len(parts) == 5 {
				kpiID := parts[4]
				if kpi, ok := state.kpiResponses[kpiID]; ok {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					_ = json.NewEncoder(w).Encode(kpi)
					return
				}
			}
		}

		http.NotFound(w, r)
	}))
}

func executeGetEntityAlertRules(
	t *testing.T,
	state *entityAlertRulesTestServerState,
	args GetEntityAlertRulesArgs,
) (string, *mcp.CallToolResult, error) {
	t.Helper()

	server := newEntityAlertRulesTestServer(t, state)
	defer server.Close()

	cfg := models.Config{
		APIBaseURL: server.URL,
		OrgSlug:    "last9",
		ClusterID:  "cluster-1",
	}
	cfg.TokenManager = &auth.TokenManager{
		AccessToken: "test-access-token",
		ExpiresAt:   time.Now().Add(24 * time.Hour),
	}

	handler := NewGetEntityAlertRulesHandler(server.Client(), cfg)
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, args)
	if err != nil {
		return "", result, err
	}

	return utils.GetTextContent(t, result), result, nil
}

func TestGetEntityAlertRulesHandler_MissingEntityID(t *testing.T) {
	state := &entityAlertRulesTestServerState{}
	text, result, err := executeGetEntityAlertRules(t, state, GetEntityAlertRulesArgs{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected IsError=true for missing entity_id")
	}
	if !strings.Contains(text, "entity_id is required") {
		t.Fatalf("expected error message, got: %s", text)
	}
}

func TestGetEntityAlertRulesHandler_ReturnsRules(t *testing.T) {
	entityID := "entity-abc"
	rules := AlertConfigResponse{
		{
			ID:               "rule-1",
			EntityID:         entityID,
			PrimaryIndicator: "throughput",
			State:            "active",
			Severity:         "breach",
			Algorithm:        "static_threshold",
			RuleName:         "High Throughput",
		},
		{
			ID:               "rule-2",
			EntityID:         entityID,
			PrimaryIndicator: "error_rate",
			State:            "active",
			Severity:         "warn",
			Algorithm:        "static_threshold",
			RuleName:         "Error Rate",
		},
	}

	state := &entityAlertRulesTestServerState{
		rulesByEntityID: map[string]AlertConfigResponse{entityID: rules},
	}

	text, _, err := executeGetEntityAlertRules(t, state, GetEntityAlertRulesArgs{EntityID: entityID})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if !strings.Contains(text, "Found 2 alert rules") {
		t.Fatalf("expected 2 rules in response, got:\n%s", text)
	}
	if !strings.Contains(text, "rule-1") || !strings.Contains(text, "rule-2") {
		t.Fatalf("expected both rule IDs in response, got:\n%s", text)
	}
	// Basic metadata fields are intentionally omitted — covered by get_alert_config.
	if strings.Contains(text, "State:") || strings.Contains(text, "Severity:") || strings.Contains(text, "Algorithm:") {
		t.Fatalf("expected basic metadata fields absent from focused output, got:\n%s", text)
	}
}

// patch_alert changes mute_until, recurring_mute_schedule and properties; the entity endpoint
// returns all three, so this tool is where an agent reads a patch back.
func TestGetEntityAlertRulesHandler_ShowsPatchedFields(t *testing.T) {
	entityID := "entity-abc"
	rules := AlertConfigResponse{
		{
			ID:                    "rule-1",
			RuleName:              "Patched",
			MuteUntil:             1792260000,
			RecurringMuteSchedule: json.RawMessage(`{"enabled":true,"timezone":"Asia/Kolkata","windows":[{"weekdays":["sat"],"start_time":"00:00","end_time":"23:59"}]}`),
			Properties: map[string]interface{}{
				"description": "Checkout errors",
				"runbook":     map[string]interface{}{"link": "https://runbook.example/checkout"},
			},
		},
		{ID: "rule-2", RuleName: "Untouched", RecurringMuteSchedule: json.RawMessage(`null`)},
	}
	state := &entityAlertRulesTestServerState{rulesByEntityID: map[string]AlertConfigResponse{entityID: rules}}

	text, _, err := executeGetEntityAlertRules(t, state, GetEntityAlertRulesArgs{EntityID: entityID})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	patched, untouched, _ := strings.Cut(text, "Rule 2:")
	for _, want := range []string{
		"  Mute Until: 2026-10-17 18:00:00 UTC\n",
		`  Recurring Mute Schedule: {"enabled":true,"timezone":"Asia/Kolkata","windows":[{"weekdays":["sat"],"start_time":"00:00","end_time":"23:59"}]}` + "\n",
		"    description: Checkout errors\n",
		"    runbook: map[link:https://runbook.example/checkout]\n",
	} {
		if !strings.Contains(patched, want) {
			t.Errorf("missing %q in:\n%s", want, patched)
		}
	}
	if strings.Contains(untouched, "Mute Until") || strings.Contains(untouched, "Recurring Mute Schedule") || strings.Contains(untouched, "Properties") {
		t.Errorf("unpatched rule should show no mute or properties lines:\n%s", untouched)
	}
}

func TestGetEntityAlertRulesHandler_ShowsIndefiniteMute(t *testing.T) {
	entityID := "entity-abc"
	state := &entityAlertRulesTestServerState{rulesByEntityID: map[string]AlertConfigResponse{entityID: {{ID: "rule-1", MuteUntil: -1}}}}
	text, _, err := executeGetEntityAlertRules(t, state, GetEntityAlertRulesArgs{EntityID: entityID})
	if err != nil || !strings.Contains(text, "  Mute Until: indefinitely\n") {
		t.Fatalf("err=%v text:\n%s", err, text)
	}
}

func TestGetEntityAlertRulesHandler_SeverityFilter(t *testing.T) {
	entityID := "entity-abc"
	rules := AlertConfigResponse{
		{ID: "rule-1", EntityID: entityID, Severity: "breach", State: "active", Algorithm: "static_threshold"},
		{ID: "rule-2", EntityID: entityID, Severity: "warn", State: "active", Algorithm: "static_threshold"},
	}

	state := &entityAlertRulesTestServerState{
		rulesByEntityID: map[string]AlertConfigResponse{entityID: rules},
	}

	text, _, err := executeGetEntityAlertRules(t, state, GetEntityAlertRulesArgs{
		EntityID: entityID,
		Severity: "breach",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if !strings.Contains(text, "Found 1 alert rules") {
		t.Fatalf("expected 1 rule after severity filter, got:\n%s", text)
	}
	if strings.Contains(text, "rule-2") {
		t.Fatalf("severity filter let warn rule through, got:\n%s", text)
	}
	if strings.Contains(text, "Severity:") {
		t.Fatalf("severity field should not appear in focused output, got:\n%s", text)
	}
}

func TestGetEntityAlertRulesHandler_KPIResolution(t *testing.T) {
	entityID := "entity-abc"
	kpiID := "kpi-uuid-1"
	rules := AlertConfigResponse{
		{
			ID:               "rule-1",
			EntityID:         entityID,
			PrimaryIndicator: "p99_latency",
			Expression:       "p99_latency",
			Condition:        "expr > 500",
			State:            "active",
			Severity:         "breach",
			Algorithm:        "static_threshold",
			RuleName:         "High P99",
			ExpressionArgs: map[string]AlertRuleExpressionArg{
				"p99_latency": {ID: kpiID},
			},
		},
	}

	t.Run("PromQL resolved", func(t *testing.T) {
		state := &entityAlertRulesTestServerState{
			rulesByEntityID: map[string]AlertConfigResponse{entityID: rules},
			kpiResponses: map[string]kpiResponse{
				kpiID: {
					ID:   kpiID,
					Name: "p99_latency",
					Definition: kpiDefinition{
						Query: `histogram_quantile(0.99, rate(http_request_duration_seconds_bucket[5m]))`,
						Unit:  "seconds",
					},
				},
			},
		}

		text, _, err := executeGetEntityAlertRules(t, state, GetEntityAlertRulesArgs{EntityID: entityID})
		if err != nil {
			t.Fatalf("handler error: %v", err)
		}
		if !strings.Contains(text, "histogram_quantile") {
			t.Fatalf("expected PromQL in response, got:\n%s", text)
		}
		if !strings.Contains(text, "Unit: seconds") {
			t.Fatalf("expected unit in response, got:\n%s", text)
		}
	})

	t.Run("KPI 404 shows lookup failure", func(t *testing.T) {
		state := &entityAlertRulesTestServerState{
			rulesByEntityID: map[string]AlertConfigResponse{entityID: rules},
			kpiResponses:    map[string]kpiResponse{}, // no KPIs → 404
		}

		text, _, err := executeGetEntityAlertRules(t, state, GetEntityAlertRulesArgs{EntityID: entityID})
		if err != nil {
			t.Fatalf("handler should succeed with KPI failure: %v", err)
		}
		if !strings.Contains(text, "lookup failed") {
			t.Fatalf("expected lookup failure note, got:\n%s", text)
		}
		if !strings.Contains(text, "rule-1") {
			t.Fatalf("rule should still appear on KPI failure, got:\n%s", text)
		}
	})
}

func TestGetEntityAlertRulesHandler_APIError(t *testing.T) {
	state := &entityAlertRulesTestServerState{
		entityStatus: http.StatusInternalServerError,
	}

	_, _, err := executeGetEntityAlertRules(t, state, GetEntityAlertRulesArgs{EntityID: "entity-abc"})
	if err == nil {
		t.Fatal("expected error for 500 response, got nil")
	}
	if !strings.Contains(err.Error(), fmt.Sprintf("%d", http.StatusInternalServerError)) {
		t.Fatalf("expected 500 in error, got: %v", err)
	}
}
