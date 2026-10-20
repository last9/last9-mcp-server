package workflows

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"text/template"

	"last9-mcp/internal/prompts"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// lineStartingWith returns the single rendered line whose trimmed text starts
// with prefix (fails otherwise) — used to assert a step's calls stay in one
// numbered list item.
func lineStartingWith(t *testing.T, text, prefix string) string {
	t.Helper()
	var found string
	n := 0
	for _, ln := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(ln), prefix) {
			found = ln
			n++
		}
	}
	if n != 1 {
		t.Fatalf("want exactly one line starting %q, found %d in:\n%s", prefix, n, text)
	}
	return found
}

// assertChecksRecentChange requires get_service_performance_details, then
// get_change_events, then analyze_change_impact, and both verdict rules: only
// `no significant change` clears a change; `possible change` and `insufficient data` keep it a suspect.
func assertChecksRecentChange(t *testing.T, name, text string) {
	t.Helper()
	perf := strings.Index(text, "get_service_performance_details")
	events := strings.Index(text, "get_change_events")
	impact := strings.Index(text, "analyze_change_impact")
	if perf < 0 || !(perf < events && events < impact) {
		t.Fatalf("%s: want get_service_performance_details, then get_change_events, then analyze_change_impact:\n%s", name, text)
	}
	for _, rule := range []string{"`no significant change` rules the change out", "`possible change` and `insufficient data` do not"} {
		if !strings.Contains(text, rule) {
			t.Fatalf("%s: the prompt must say %q:\n%s", name, rule, text)
		}
	}
}

// handlerText calls a workflow's handler and returns the single message's text.
func handlerText(t *testing.T, w Workflow) string {
	t.Helper()
	res, err := w.Handler(context.Background(), &mcp.GetPromptRequest{})
	if err != nil {
		t.Fatalf("%s handler returned error: %v", w.Name, err)
	}
	if len(res.Messages) != 1 {
		t.Fatalf("%s handler: got %d messages, want 1", w.Name, len(res.Messages))
	}
	tc, ok := res.Messages[0].Content.(*mcp.TextContent)
	if !ok {
		t.Fatalf("%s handler: message content is not *mcp.TextContent", w.Name)
	}
	return tc.Text
}

// TestWorkflowMetadata covers the two parameter-less workflows; the
// parameterized ones assert their own metadata in per-workflow files.
func TestWorkflowMetadata(t *testing.T) {
	cases := []struct {
		w     Workflow
		name  string
		title string
	}{
		{ScopedLogAttributeDiscovery, "scoped-log-attribute-discovery", "Scoped Log Attribute Discovery"},
		{ExceptionRootCauseInvestigation, "exception-root-cause-investigation", "Exception Root Cause Investigation"},
	}
	for _, c := range cases {
		if c.w.Name != c.name {
			t.Errorf("Name = %q, want %q", c.w.Name, c.name)
		}
		if c.w.Title != c.title {
			t.Errorf("%s: Title = %q, want %q", c.name, c.w.Title, c.title)
		}
		if c.w.Description == "" {
			t.Errorf("%s: Description is empty", c.name)
		}
		if c.w.Handler == nil {
			t.Errorf("%s: Handler is nil", c.name)
		}
		if c.w.Arguments != nil {
			t.Errorf("%s: Arguments = %v, want nil (this workflow takes no arguments)", c.name, c.w.Arguments)
		}
	}
}

func TestWorkflowHandlerReturnsEmbeddedText(t *testing.T) {
	if got := handlerText(t, ScopedLogAttributeDiscovery); got != prompts.ScopedLogAttributeDiscoveryWorkflow {
		t.Errorf("scoped-log-attribute-discovery handler text does not match embedded prompt")
	}
	if got := handlerText(t, ExceptionRootCauseInvestigation); got != prompts.ExceptionRootCauseInvestigationWorkflow {
		t.Errorf("exception-root-cause-investigation handler text does not match embedded prompt")
	}
}

// Guards the routing language inside the workflow prompt bodies.
func TestWorkflowPromptsContainRoutingGuards(t *testing.T) {
	checks := []struct {
		name   string
		text   string
		phrase string
	}{
		{"scoped log attribute discovery", prompts.ScopedLogAttributeDiscoveryWorkflow, "get_service_profile"},
		{"scoped log attribute discovery", prompts.ScopedLogAttributeDiscoveryWorkflow, "Forbidden tool for this workflow: `get_service_logs`"},
		{"exception root cause investigation", prompts.ExceptionRootCauseInvestigationWorkflow, "get_service_profile"},
		{"exception root cause investigation", prompts.ExceptionRootCauseInvestigationWorkflow, "AGGREGATE FIRST"},
		{"exception root cause investigation", prompts.ExceptionRootCauseInvestigationWorkflow, "severity_set in (\"none\", \"partial\")"},
	}
	for _, c := range checks {
		if !strings.Contains(c.text, c.phrase) {
			t.Errorf("%s workflow prompt missing %q", c.name, c.phrase)
		}
	}
}

// Every other assertion greps a string inside the one file it was added to, so
// a workflow can drift from the tri-state contract in references/investigation.md
// without any test noticing. These two check across files.

func TestWorkflowsGateOnAbsentNotPresent(t *testing.T) {
	// `unknown` means not measured. Gating on == "present" sends it down the
	// skip branch, suppressing tools on every service whose tier did not resolve.
	for name, body := range map[string]string{
		"diagnose_error_rate":                prompts.DiagnoseErrorRateWorkflow,
		"exception_root_cause_investigation": prompts.ExceptionRootCauseInvestigationWorkflow,
		"investigate_latency_spike":          prompts.InvestigateLatencySpikeWorkflow,
		"on_call_runbook":                    prompts.OnCallRunbookWorkflow,
		"scoped_log_attribute_discovery":     prompts.ScopedLogAttributeDiscoveryWorkflow,
		"references/investigation":           prompts.InvestigationReference,
	} {
		// No exemption: an escape hatch here is another condition that can
		// silently stop failing, which is the bug this test exists to catch.
		for _, signal := range []string{"traces", "logs", "metrics"} {
			if gate := "telemetry." + signal + ` == "present"`; strings.Contains(body, gate) {
				t.Errorf("%s gates on %s; use != \"absent\" so unknown does not suppress tools", name, gate)
			}
		}
	}
}

func TestInvestigationReferenceKeepsUnknownDistinctFromAbsent(t *testing.T) {
	if !strings.Contains(prompts.InvestigationReference, "`unknown` means not measured") {
		t.Error("investigation reference must keep unknown distinct from absent")
	}
}

// level_field and parse_hint are nested under signal_shape in the profile JSON
// (see signalShapeResponse). A flattened profile.<field> path names something
// the response does not contain, and the model queries on it anyway.
func TestWorkflowsUseNestedSignalShapePaths(t *testing.T) {
	for name, body := range map[string]string{
		"diagnose_error_rate":                prompts.DiagnoseErrorRateWorkflow,
		"exception_root_cause_investigation": prompts.ExceptionRootCauseInvestigationWorkflow,
		"investigate_latency_spike":          prompts.InvestigateLatencySpikeWorkflow,
		"on_call_runbook":                    prompts.OnCallRunbookWorkflow,
		"scoped_log_attribute_discovery":     prompts.ScopedLogAttributeDiscoveryWorkflow,
		"partials":                           prompts.WorkflowPartials,
		"references/investigation":           prompts.InvestigationReference,
	} {
		for _, field := range []string{"level_field", "parse_hint", "severity_set", "log_format"} {
			if bad := "profile." + field; strings.Contains(body, bad) {
				t.Errorf("%s uses %s; the field is nested under signal_shape", name, bad)
			}
		}
	}
}

// Error triage workflows render one shared log-novelty define, so a wording
// fix lands in every workflow at once.
func TestErrorWorkflowsRenderTheSharedLogNoveltyText(t *testing.T) {
	args := map[string]string{"service": "checkout", "env": "prod", "time": "1h"}
	partials := template.Must(template.New("partials").Parse(prompts.WorkflowPartials))
	var shared bytes.Buffer
	if err := partials.ExecuteTemplate(&shared, "logNovelty", args); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(shared.String(), "get_emerging_log_patterns(service_name=checkout, env=prod)") {
		t.Fatalf("logNovelty should render the scoped novelty call:\n%s", shared.String())
	}

	diagnose, err := DiagnoseErrorRate.Handler(context.Background(), &mcp.GetPromptRequest{Params: &mcp.GetPromptParams{Arguments: args}})
	if err != nil {
		t.Fatal(err)
	}
	for name, got := range map[string]string{
		"diagnose-error-rate":    diagnose.Messages[0].Content.(*mcp.TextContent).Text,
		"on-call-runbook errors": renderOnCallRunbook(t, map[string]string{"symptom": "errors", "service": "checkout", "env": "prod", "time": "1h"}),
		"on-call-runbook weird":  renderOnCallRunbook(t, map[string]string{"symptom": "weird", "service": "checkout", "env": "prod", "time": "1h"}),
	} {
		if !strings.Contains(got, shared.String()) {
			t.Errorf("%s should render the shared logNovelty text verbatim:\n%s", name, got)
		}
	}
}
