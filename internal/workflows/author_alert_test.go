package workflows

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func renderAuthorAlert(t *testing.T, args map[string]string) string {
	t.Helper()
	res, err := AuthorAlert.Handler(context.Background(), &mcp.GetPromptRequest{
		Params: &mcp.GetPromptParams{Arguments: args},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return res.Messages[0].Content.(*mcp.TextContent).Text
}

const promoteCall = "patch_alert(entity_id, id, is_disabled=false)"

// assertInOrder fails unless each needle appears, first occurrences in order.
func assertInOrder(t *testing.T, text string, needles ...string) {
	t.Helper()
	last := -1
	for _, needle := range needles {
		i := strings.Index(text, needle)
		if i < 0 {
			t.Fatalf("missing %q in:\n%s", needle, text)
		}
		if i < last {
			t.Fatalf("%q appears before the previous step in:\n%s", needle, text)
		}
		last = i
	}
}

func TestAuthorAlertMetadata(t *testing.T) {
	w := AuthorAlert
	if w.Name != "author-alert" || w.Title == "" || w.Description == "" || w.Handler == nil {
		t.Fatalf("metadata = %+v", w)
	}
	required := map[string]bool{}
	for _, a := range w.Arguments {
		required[a.Name] = a.Required
	}
	if !required["goal"] {
		t.Error("goal must be a required argument")
	}
	for _, optional := range []string{"alert_group", "dashboard_id", "panel"} {
		if v, ok := required[optional]; !ok || v {
			t.Errorf("%s must be declared optional", optional)
		}
	}
}

func TestAuthorAlertMissingGoal(t *testing.T) {
	if _, err := AuthorAlert.Handler(context.Background(), &mcp.GetPromptRequest{
		Params: &mcp.GetPromptParams{Arguments: map[string]string{"alert_group": "checkout"}},
	}); err == nil {
		t.Fatal("expected error when goal is missing")
	}
}

func TestAuthorAlertFlowAndGates(t *testing.T) {
	got := renderAuthorAlert(t, map[string]string{"goal": "checkout p99 latency"})
	assertInOrder(t, got,
		"1. Find the alert group", "call get_alert_groups", "2.", "Call get_entity_kpis",
		"3. Ground the threshold", "call prometheus_range_query", "4. Call recommend_alert_config",
		"5. Call validate_alert_expression", "6. Call create_alert", "7. Read it back: call get_entity_alert_rules",
		"8. Show the user the draft", promoteCall)
	for _, gate := range []string{
		"confirm this session exposes the tools needed",
		"If a required tool is unavailable",
		"last9://reference/alerting",
		"Never invent a KPI id",
		"call create_kpi once",
		`Do not continue until it returns {"valid":true}`,
		"never call create_alert again",
		"Call patch_alert(entity_id, id, is_disabled=false) only after the user explicitly confirms",
		"Do not call mute_alert_group",
		"key each indicator by its KPI's exact name",
		"If a multi-indicator expression returns \"data source client not in context\"",
	} {
		if !strings.Contains(got, gate) {
			t.Errorf("render missing gate %q:\n%s", gate, got)
		}
	}
	if strings.Contains(got, "call get_dashboard(") {
		t.Errorf("without dashboard_id the render must not route through get_dashboard:\n%s", got)
	}
	if !strings.Contains(got, "if more than one fits, ask the user") {
		t.Errorf("without alert_group the render must ask when several groups fit:\n%s", got)
	}
}

func TestAuthorAlertRendersAlertGroupBranch(t *testing.T) {
	got := renderAuthorAlert(t, map[string]string{"goal": "error rate", "alert_group": "checkout"})
	if !strings.Contains(got, `get_alert_groups(alert_group_name="checkout")`) {
		t.Errorf("alert_group must filter get_alert_groups:\n%s", got)
	}
}

func TestAuthorAlertRendersDashboardPanelBranch(t *testing.T) {
	got := renderAuthorAlert(t, map[string]string{"goal": "panel latency", "dashboard_id": "dash-1", "panel": "p99"})
	assertInOrder(t, got,
		"1. Find the alert group", "call get_alert_groups", "2. Alert from a dashboard panel:",
		`call get_dashboard(id="dash-1")`, `panel "p99"`, "Then call get_entity_kpis", "call create_kpi once",
		"3. Ground the threshold", "call prometheus_range_query", "5. Call validate_alert_expression",
		"6. Call create_alert", "7. Read it back: call get_entity_alert_rules", promoteCall)
	if !strings.Contains(got, "resolve any dashboard variables") {
		t.Errorf("panel branch must resolve dashboard variables before create_kpi:\n%s", got)
	}
}
