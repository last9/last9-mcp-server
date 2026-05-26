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
		{"{{labels}}", "must have {{labels}} placeholder so buildEnhancedDescription can inject real attribute names"},
	}
	for _, c := range checks {
		if !strings.Contains(inst, c.phrase) {
			t.Errorf("GetServiceLogsInstructions missing %q: %s", c.phrase, c.reason)
		}
	}
}
