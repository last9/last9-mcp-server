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

func TestAPMServiceDeviationsDescription(t *testing.T) {
	description := strings.ToLower(prompts.GetAPMServiceDeviationsDescription)
	checks := []struct {
		phrase string
		reason string
	}{
		{"equal-duration baseline", "must describe comparative rather than snapshot behavior"},
		{"get_service_summary", "must disambiguate one-window snapshots"},
		{"service_name", "must explain fleet and service scope"},
		{"environments remain separate", "must prevent merged environment conclusions"},
		{"server-request workloads", "must state the V1 workload boundary"},
		{"unsupported_workload_shape", "must name the unsupported workload outcome"},
		{"datasource", "must document datasource selection"},
		{"baseline_start_time_iso", "must document explicit baseline windows"},
		{"max_services", "must document fleet result bounds"},
		{"max_operations", "must document operation result bounds"},
		{"regressions", "must describe regression results"},
		{"improvements", "must describe improvement results"},
		{"stable", "must describe stable results"},
		{"evidence quality", "must describe categorical evidence quality"},
		{"does not establish contribution, attribution, cause, or root cause", "must prevent causal overclaiming"},
	}
	for _, check := range checks {
		if !strings.Contains(description, check.phrase) {
			t.Errorf("description missing %q: %s", check.phrase, check.reason)
		}
	}
}
