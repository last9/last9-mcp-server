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
					_ = json.NewEncoder(w).Encode(rules)
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
