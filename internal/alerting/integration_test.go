package alerting

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const integrationTestTimeout = 30 * time.Second

func TestGetAlertConfigHandler_Integration_Basic(t *testing.T) {
	cfg := utils.SetupTestConfigOrSkip(t)
	handler := NewGetAlertConfigHandler(http.DefaultClient, *cfg)

	ctx, cancel := context.WithTimeout(context.Background(), integrationTestTimeout)
	defer cancel()

	result, _, err := handler(ctx, &mcp.CallToolRequest{}, GetAlertConfigArgs{})
	if utils.CheckAPIError(t, err) {
		return
	}

	text := utils.GetTextContent(t, result)
	t.Logf("response:\n%s", text)

	if !strings.Contains(text, "Found") {
		t.Fatalf("expected 'Found N alert rules' in response, got:\n%s", text)
	}

	// If no rules exist, skip the deeper assertions.
	if strings.Contains(text, "Found 0 alert rules") {
		t.Skip("no alert rules in org; skipping field assertions")
	}

	// Every rule must have at minimum an ID and state.
	if !strings.Contains(text, "ID:") {
		t.Fatalf("expected at least one rule with 'ID:' in response, got:\n%s", text)
	}
	if !strings.Contains(text, "State:") {
		t.Fatalf("expected 'State:' in response, got:\n%s", text)
	}
}

func TestGetAlertConfigHandler_Integration_KPIResolution(t *testing.T) {
	cfg := utils.SetupTestConfigOrSkip(t)
	handler := NewGetAlertConfigHandler(http.DefaultClient, *cfg)

	ctx, cancel := context.WithTimeout(context.Background(), integrationTestTimeout)
	defer cancel()

	result, _, err := handler(ctx, &mcp.CallToolRequest{}, GetAlertConfigArgs{})
	if utils.CheckAPIError(t, err) {
		return
	}

	text := utils.GetTextContent(t, result)
	t.Logf("response:\n%s", text)

	if strings.Contains(text, "Found 0 alert rules") {
		t.Skip("no alert rules in org; skipping KPI resolution assertions")
	}

	// At least one rule should have expression_args (static threshold rules created
	// via the API or Terraform all have them). If we see "Indicators:" in the output
	// we know resolution ran. If no rule has indicators, that is also valid (all
	// Grafana-synced rules) — we just skip rather than fail.
	if !strings.Contains(text, "Indicators:") {
		t.Skip("no rules with expression_args found; skipping KPI resolution check")
	}

	// For every rule that has indicators, there must be either a PromQL line or a
	// lookup failure note — never a silent empty field.
	indicatorBlocks := strings.Split(text, "Indicators:")
	for _, block := range indicatorBlocks[1:] { // skip text before first "Indicators:"
		if !strings.Contains(block, "PromQL:") && !strings.Contains(block, "lookup failed") {
			t.Fatalf("indicator block has neither PromQL nor lookup failure note:\n%s", block)
		}
	}
}

func TestGetAlertConfigHandler_Integration_SeverityFilter(t *testing.T) {
	cfg := utils.SetupTestConfigOrSkip(t)
	handler := NewGetAlertConfigHandler(http.DefaultClient, *cfg)

	ctx, cancel := context.WithTimeout(context.Background(), integrationTestTimeout)
	defer cancel()

	result, _, err := handler(ctx, &mcp.CallToolRequest{}, GetAlertConfigArgs{Severity: "breach"})
	if utils.CheckAPIError(t, err) {
		return
	}

	text := utils.GetTextContent(t, result)
	t.Logf("response:\n%s", text)

	// Every rule in response must have severity breach (case-insensitive).
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "Severity:") {
			severity := strings.TrimSpace(strings.TrimPrefix(trimmed, "Severity:"))
			if !strings.EqualFold(severity, "breach") {
				t.Fatalf("severity filter returned non-breach rule with severity %q", severity)
			}
		}
	}
}

func TestGetEntityAlertRulesHandler_Integration(t *testing.T) {
	cfg := utils.SetupTestConfigOrSkip(t)

	// First fetch the org-wide list to get a real entity ID.
	ctx, cancel := context.WithTimeout(context.Background(), integrationTestTimeout)
	defer cancel()

	rules, err := fetchAlertConfig(ctx, http.DefaultClient, *cfg)
	if err != nil {
		t.Skipf("could not fetch org alert config: %v", err)
	}
	if len(rules) == 0 {
		t.Skip("no alert rules in org; skipping")
	}

	entityID := rules[0].EntityID
	if entityID == "" {
		t.Skip("first rule has no entity_id; skipping")
	}

	handler := NewGetEntityAlertRulesHandler(http.DefaultClient, *cfg)
	result, _, err := handler(ctx, &mcp.CallToolRequest{}, GetEntityAlertRulesArgs{EntityID: entityID})
	if utils.CheckAPIError(t, err) {
		return
	}

	text := utils.GetTextContent(t, result)
	t.Logf("entity_id=%s response:\n%s", entityID, text)

	if !strings.Contains(text, "Found") {
		t.Fatalf("expected 'Found N alert rules' in response, got:\n%s", text)
	}
	if !strings.Contains(text, "ID:") {
		t.Fatalf("expected at least one rule with 'ID:' in response, got:\n%s", text)
	}
	// Basic metadata fields are omitted from this focused tool.
	if strings.Contains(text, "State:") || strings.Contains(text, "Severity:") {
		t.Fatalf("basic metadata fields should be absent from entity alert rules output, got:\n%s", text)
	}

	// If any indicators present, each must have PromQL or a lookup failure note.
	// Split on "Indicators:" — each block runs from one marker to the next, containing
	// that rule's indicator sub-section followed by the next rule's fields.
	// No truncation needed: "PromQL:" and "lookup failed" only appear inside indicators.
	if strings.Contains(text, "Indicators:") {
		indicatorBlocks := strings.Split(text, "Indicators:")
		for _, block := range indicatorBlocks[1:] {
			if !strings.Contains(block, "PromQL:") && !strings.Contains(block, "lookup failed") {
				t.Fatalf("indicator block has neither PromQL nor lookup failure note:\n%s", block)
			}
		}
	}
}

func TestAlertRuleStateHandler_Integration(t *testing.T) {
	cfg := utils.SetupTestConfigOrSkip(t)
	handler := NewAlertRuleStateHandler(http.DefaultClient, *cfg)

	ctx, cancel := context.WithTimeout(context.Background(), integrationTestTimeout)
	defer cancel()

	// 5-minute window at 60s resolution → 6 samples (well under the 100 cap).
	end := time.Now().Unix()
	start := end - 5*60
	args := AlertRuleStateRequest{
		StartTime: start,
		EndTime:   end,
		Step:      60,
	}

	result, _, err := handler(ctx, &mcp.CallToolRequest{}, args)
	if utils.CheckAPIError(t, err) {
		return
	}
	if result == nil {
		t.Fatalf("expected non-nil result")
	}
	if result.IsError {
		text := utils.GetTextContent(t, result)
		t.Fatalf("unexpected IsError result: %s", text)
	}

	text := utils.GetTextContent(t, result)
	t.Logf("response (truncated to 500 chars):\n%s", truncate(text, 500))

	// Output must be a JSON object — map[ruleID][]Datapoint. Empty {} is valid (no rules in window).
	var decoded map[string][]struct {
		Timestamp int64 `json:"timestamp"`
		IsFiring  int   `json:"is_firing"`
	}
	if err := json.Unmarshal([]byte(text), &decoded); err != nil {
		t.Fatalf("expected JSON map output, got error: %v\npayload: %s", err, text)
	}

	if len(decoded) == 0 {
		t.Skip("no alert rules surfaced in the last 5 minutes; nothing to assert on")
	}

	// Every rule's timeseries must have exactly the expected number of samples
	// (inclusive: (end-start)/step + 1) and contiguous timestamps at `step` intervals.
	expectedSamples := (end-start)/60 + 1
	for ruleID, dps := range decoded {
		if int64(len(dps)) != expectedSamples {
			t.Errorf("rule %s: expected %d samples, got %d", ruleID, expectedSamples, len(dps))
			continue
		}
		for i, dp := range dps {
			wantT := start + int64(i)*60
			if dp.Timestamp != wantT {
				t.Errorf("rule %s sample %d: expected timestamp %d, got %d", ruleID, i, wantT, dp.Timestamp)
			}
			if dp.IsFiring != 0 && dp.IsFiring != 1 {
				t.Errorf("rule %s sample %d: is_firing must be 0 or 1, got %d", ruleID, i, dp.IsFiring)
			}
		}
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
