package prompts_test

import (
	"strings"
	"testing"

	"last9-mcp/internal/prompts"
)

func TestGetServiceLogsDescriptionCriticalRules(t *testing.T) {
	desc := prompts.GetServiceLogsDescription
	if desc == "" {
		t.Fatal("GetServiceLogsDescription is empty — embed directive missing")
	}
	checks := []struct {
		phrase string
		reason string
	}{
		{"get_logs", "must tell model to prefer get_logs for structured attribute queries"},
		{"get_log_attributes", "must tell model to check attributes before body_filters"},
		{"body_filters", "must explicitly label body_filters as last resort"},
		{"last9://reference/service_logs", "must point at the service_logs resource"},
		{"get_log_attributes", "must point at discovery before structured filters"},
	}
	for _, c := range checks {
		if !strings.Contains(desc, c.phrase) {
			t.Errorf("GetServiceLogsDescription missing %q: %s", c.phrase, c.reason)
		}
	}
}

func TestWhaleDescriptionsBounded(t *testing.T) {
	for name, body := range map[string]string{
		"get_logs_base":         prompts.GetLogsDescription,
		"get_traces_base":       prompts.GetTracesDescription,
		"get_service_logs_base": prompts.GetServiceLogsDescription,
	} {
		if len(body) == 0 {
			t.Errorf("%s empty", name)
		}
		if len(body) > 2000 {
			t.Errorf("%s length %d exceeds 2000-char tripwire", name, len(body))
		}
	}
}

func TestReferenceManualsEmbedded(t *testing.T) {
	for name, body := range map[string]string{
		"logjson":      prompts.LogjsonReference,
		"tracejson":    prompts.TracejsonReference,
		"service_logs": prompts.ServiceLogsReference,
	} {
		if len(body) < 1000 {
			t.Errorf("%s reference too short (%d chars) — embed may be wrong", name, len(body))
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
		{"call this tool first and by itself", "must prevent speculative parallel investigation"},
		{"do not batch speculative corroboration", "must require inspecting the comparison before follow-ups"},
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
		{"operation_apdex_reconciliations", "must explain request-weighted operation evidence"},
		{"unexplained_delta", "must preserve incomplete operation coverage as an explicit residual"},
		{"reported coverage", "must prevent treating partial operation evidence as complete"},
		{"does not establish contribution, attribution, cause, or root cause", "must prevent causal overclaiming"},
	}
	for _, check := range checks {
		if !strings.Contains(description, check.phrase) {
			t.Errorf("description missing %q: %s", check.phrase, check.reason)
		}
	}
}

func TestAPMServiceDeviationsDescriptionDefaultsAndPartialResults(t *testing.T) {
	description := strings.ToLower(prompts.GetAPMServiceDeviationsDescription)
	for _, phrase := range []string{
		"max_services` and `max_operations` each default to 10 and cannot exceed 10",
		"partial_errors",
		"successful evidence remains usable",
		"explicitly qualify conclusions",
		"state the returned evidence quality or limitations",
		"stable`, `no_data`, and `unsupported_workload_shape` as terminal",
		"do not automatically call follow-up tools",
		"all metric queries fail, the tool returns an error",
	} {
		if !strings.Contains(description, phrase) {
			t.Errorf("description missing exact contract wording %q", phrase)
		}
	}
}
