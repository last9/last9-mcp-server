package alerting

import (
	"context"
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
