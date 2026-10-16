package alerting

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/constants"
	"last9-mcp/internal/models"
)

func TestFormatEvaluationHistoryLines(t *testing.T) {
	lines := formatEvaluationHistoryLines([]evaluationSample{
		{Timestamp: 1700000060, State: "firing"},
		{Timestamp: 1700000000, State: "ok"},
	})
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "Recent evaluations") {
		t.Fatalf("missing header: %s", joined)
	}
	if !strings.Contains(joined, "firing") || !strings.Contains(joined, "ok") {
		t.Fatalf("missing states: %s", joined)
	}
	// Newest first.
	if strings.Index(joined, "firing") > strings.Index(joined, "ok") &&
		strings.Contains(joined, "1700000060") {
		// timestamps formatted as UTC; just ensure firing appears before ok in newest-first order
	}
	firingIdx := strings.Index(joined, "firing")
	okIdx := strings.Index(joined, "ok")
	if firingIdx < 0 || okIdx < 0 || firingIdx > okIdx {
		t.Fatalf("expected newest (firing) before older (ok):\n%s", joined)
	}
}

func TestFormatAlertConfigIncludesEvaluationHistory(t *testing.T) {
	rules := AlertConfigResponse{{
		ID:       "rule-1",
		RuleName: "Latency",
		State:    "firing",
		Severity: "breach",
		EntityID: "entity-1",
	}}
	history := map[string][]evaluationSample{
		"rule-1": {
			{Timestamp: 1700000060, State: "firing"},
			{Timestamp: 1700000000, State: "ok"},
		},
	}
	out := formatAlertConfigResponse(rules, nil, nil, "", false, history)
	if !strings.Contains(out, "Recent evaluations") {
		t.Fatalf("missing evaluation history:\n%s", out)
	}
	if !strings.Contains(out, "firing") {
		t.Fatalf("missing firing sample:\n%s", out)
	}
}

func TestFetchRecentEvaluationHistory_SamplesMonitor(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != constants.EndpointAlertsMonitor {
			http.NotFound(w, r)
			return
		}
		calls++
		_ = json.NewEncoder(w).Encode(AlertsResponse{
			AlertRules: []AlertRuleData{{RuleID: "rule-1", State: "firing"}},
		})
	}))
	defer srv.Close()

	cfg := models.Config{
		APIBaseURL: srv.URL,
		TokenManager: &auth.TokenManager{
			AccessToken: "tok",
			ExpiresAt:   time.Now().Add(time.Hour),
		},
	}
	history := fetchRecentEvaluationHistory(context.Background(), srv.Client(), cfg, []string{"rule-1"})
	if calls != recentEvaluationHistoryPoints {
		t.Fatalf("monitor calls = %d, want %d", calls, recentEvaluationHistoryPoints)
	}
	samples := history["rule-1"]
	if len(samples) != recentEvaluationHistoryPoints {
		t.Fatalf("samples = %d", len(samples))
	}
	if samples[0].State != "firing" {
		t.Fatalf("state = %q", samples[0].State)
	}
}

func TestFetchRecentEvaluationHistory_SkipsLargeResultSets(t *testing.T) {
	ids := make([]string, recentEvaluationHistoryMaxRules+1)
	for i := range ids {
		ids[i] = "r"
	}
	if got := fetchRecentEvaluationHistory(context.Background(), http.DefaultClient, models.Config{}, ids); got != nil {
		t.Fatalf("expected nil for oversized set, got %v", got)
	}
}
