package prompts_test

import (
	"strings"
	"testing"

	"last9-mcp/internal/prompts"
)

func TestGetServiceLogsInstructionsEmbedded(t *testing.T) {
	if prompts.GetServiceLogsInstructions == "" {
		t.Fatal("GetServiceLogsInstructions is empty — embed directive missing in prompts.go")
	}
}

func TestGetServiceLogsInstructionsContainsDisambiguation(t *testing.T) {
	inst := prompts.GetServiceLogsInstructions
	checks := []struct {
		phrase string
		reason string
	}{
		{"get_logs", "must tell model to prefer get_logs for structured attribute queries"},
		{"get_log_attributes", "must tell model to check attributes first before using body_filters"},
		{"body_filters", "must explicitly label body_filters as last resort / plain-text only"},
		{"NEVER use this tool to answer aggregate questions", "must prevent raw samples from replacing aggregate log queries"},
		{"Do not fall back to `get_service_logs`", "must tell the model to retry/fix get_logs rather than sampling raw rows"},
		{"{{labels}}", "must have {{labels}} placeholder so buildEnhancedDescription can inject real attribute names"},
	}
	for _, c := range checks {
		if !strings.Contains(inst, c.phrase) {
			t.Errorf("GetServiceLogsInstructions missing %q: %s", c.phrase, c.reason)
		}
	}
}

func TestGetServiceLogsBaseDescriptionSaysRawSamplesOnly(t *testing.T) {
	desc := prompts.GetServiceLogsDescription
	checks := []struct {
		phrase string
		reason string
	}{
		{"small sample of raw log entries", "must position get_service_logs as sample drilldown, not analytics"},
		{"Do not use this tool for counts", "must route aggregate questions away from get_service_logs early in the description"},
		{"fix or simplify the get_logs pipeline", "must prevent fallback to raw samples after get_logs errors"},
	}
	for _, c := range checks {
		if !strings.Contains(desc, c.phrase) {
			t.Errorf("GetServiceLogsDescription missing %q: %s", c.phrase, c.reason)
		}
	}
}

func TestWorkflowPromptsEmbedded(t *testing.T) {
	checks := []struct {
		name   string
		text   string
		phrase string
	}{
		{
			name:   "scoped log attribute discovery",
			text:   prompts.ScopedLogAttributeDiscoveryWorkflow,
			phrase: "Forbidden tool for this workflow: `get_service_logs`",
		},
		{
			name:   "exception log continuation",
			text:   prompts.ExceptionLogContinuationWorkflow,
			phrase: "Do not call `get_service_logs`",
		},
	}
	for _, c := range checks {
		if !strings.Contains(c.text, c.phrase) {
			t.Errorf("%s workflow prompt missing %q", c.name, c.phrase)
		}
	}
}
