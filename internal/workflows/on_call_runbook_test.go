package workflows

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestOnCallRunbookMetadata(t *testing.T) {
	w := OnCallRunbook
	if w.Name != "on-call-runbook" {
		t.Errorf("Name = %q", w.Name)
	}
	if w.Handler == nil || w.Title == "" || w.Description == "" {
		t.Error("Title/Description/Handler must be set")
	}
	required := map[string]bool{}
	for _, a := range w.Arguments {
		if a.Required {
			required[a.Name] = true
		}
	}
	for _, k := range []string{"symptom", "time"} {
		if !required[k] {
			t.Errorf("arg %q must be Required", k)
		}
	}
}

func TestOnCallRunbookMissingSymptom(t *testing.T) {
	_, err := OnCallRunbook.Handler(context.Background(), &mcp.GetPromptRequest{
		Params: &mcp.GetPromptParams{Arguments: map[string]string{"time": "1h"}},
	})
	if err == nil {
		t.Fatal("expected error when 'symptom' missing")
	}
}

func TestOnCallRunbookRoutesBySymptom(t *testing.T) {
	cases := map[string]string{
		"latency":  "get_apm_service_deviations",
		"errors":   "get_exceptions",
		"database": "get_database_slow_queries",
		"weird":    "Unknown symptom",
	}
	for symptom, want := range cases {
		res, err := OnCallRunbook.Handler(context.Background(), &mcp.GetPromptRequest{
			Params: &mcp.GetPromptParams{Arguments: map[string]string{"symptom": symptom, "time": "1h", "service": "checkout"}},
		})
		if err != nil {
			t.Fatalf("symptom %q: unexpected error: %v", symptom, err)
		}
		got := res.Messages[0].Content.(*mcp.TextContent).Text
		if !strings.Contains(got, want) {
			t.Errorf("symptom %q render should contain %q:\n%s", symptom, want, got)
		}
		if !strings.Contains(got, "get_service_profile") {
			t.Errorf("symptom %q with service set should include profile bootstrap:\n%s", symptom, got)
		}
		if strings.Contains(got, "fleet triage") {
			t.Errorf("symptom %q with service set should NOT include fleet triage", symptom)
		}
	}
}

func TestOnCallRunbookNormalizesSymptom(t *testing.T) {
	// Case, whitespace, and obvious synonyms must route, not fall through to
	// the unknown branch.
	cases := map[string]string{
		"Latency":    "get_apm_service_deviations",
		"ERRORS":     "get_exceptions",
		"  error  ":  "get_exceptions",
		"exceptions": "get_exceptions",
		"DB":         "get_database_slow_queries",
	}
	for symptom, want := range cases {
		res, err := OnCallRunbook.Handler(context.Background(), &mcp.GetPromptRequest{
			Params: &mcp.GetPromptParams{Arguments: map[string]string{"symptom": symptom, "time": "1h", "service": "checkout"}},
		})
		if err != nil {
			t.Fatalf("symptom %q: unexpected error: %v", symptom, err)
		}
		got := res.Messages[0].Content.(*mcp.TextContent).Text
		if !strings.Contains(got, want) {
			t.Errorf("symptom %q should route to contain %q:\n%s", symptom, want, got)
		}
		if strings.Contains(got, "Unknown symptom") {
			t.Errorf("symptom %q should have normalized, not hit the unknown branch", symptom)
		}
	}
}

func TestOnCallRunbookFleetTriageWhenNoService(t *testing.T) {
	res, err := OnCallRunbook.Handler(context.Background(), &mcp.GetPromptRequest{
		Params: &mcp.GetPromptParams{Arguments: map[string]string{"symptom": "latency", "time": "1h"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := res.Messages[0].Content.(*mcp.TextContent).Text
	if !strings.Contains(got, "fleet triage") {
		t.Errorf("no-service render should start with fleet triage:\n%s", got)
	}
	if !strings.Contains(got, "get_service_profile") {
		t.Errorf("no-service render should call profile after fleet triage:\n%s", got)
	}
}

func renderOnCallRunbook(t *testing.T, args map[string]string) string {
	t.Helper()
	res, err := OnCallRunbook.Handler(context.Background(), &mcp.GetPromptRequest{Params: &mcp.GetPromptParams{Arguments: args}})
	if err != nil {
		t.Fatalf("args %v: unexpected error: %v", args, err)
	}
	return res.Messages[0].Content.(*mcp.TextContent).Text
}

func TestOnCallRunbookChecksLogNoveltyOnlyBeforeLogSearches(t *testing.T) {
	// Each name must appear, after the novelty check.
	cases := map[string][]string{
		"errors": {"get_service_logs", "get_logs only if", "get_log_patterns"},
		"weird":  {"get_log_patterns"},
	}
	for symptom, after := range cases {
		got := renderOnCallRunbook(t, map[string]string{"symptom": symptom, "time": "1h", "service": "checkout"})
		novelty := strings.Index(got, "get_emerging_log_patterns(service_name=checkout)")
		if novelty < 0 || !strings.Contains(got, "no log novelty") {
			t.Fatalf("symptom %q render must check log novelty before searching logs:\n%s", symptom, got)
		}
		for _, name := range after {
			if i := strings.Index(got, name); i < novelty {
				t.Errorf("symptom %q render must check log novelty before %s:\n%s", symptom, name, got)
			}
		}
	}

	// The unknown branch learns what is firing first; novelty only matters once it leads to a log search.
	got := renderOnCallRunbook(t, map[string]string{"symptom": "weird", "time": "1h", "service": "checkout"})
	if alerts, novelty := strings.Index(got, "get_alerts"), strings.Index(got, "get_emerging_log_patterns"); alerts < 0 || novelty < alerts {
		t.Errorf("unknown symptom must reach get_alerts before log novelty:\n%s", got)
	}
	if !strings.Contains(got, "If it matches latency or database, do not call the log-pattern tools.") {
		t.Errorf("unknown symptom must exclude the pattern tools for branches that read no logs:\n%s", got)
	}
	if gate, novelty := strings.Index(got, "If it matches errors: Before the first log search"), strings.Index(got, "get_emerging_log_patterns"); gate < 0 || novelty < gate {
		t.Errorf("unknown symptom must render log novelty only under an explicit errors match:\n%s", got)
	}

	// Branches that never read logs must not pay for two log samples.
	for _, symptom := range []string{"latency", "database"} {
		got := renderOnCallRunbook(t, map[string]string{"symptom": symptom, "time": "1h", "service": "checkout"})
		if strings.Contains(got, "get_emerging_log_patterns") || strings.Contains(got, "get_log_patterns") {
			t.Errorf("symptom %q reads no logs, so its render must not call the pattern tools:\n%s", symptom, got)
		}
	}
}

func TestOnCallRunbookLogNoveltyFiltersSeverityOnlyWhenSeveritySetIsAll(t *testing.T) {
	// Error triage should not let info/debug churn crowd error templates out of top_n,
	// but severity_filters drop body-level lines when severity_set is none or partial.
	const rule = `severity_filters=["error", "fatal", "critical"] only when severity_set is "all"`
	for _, symptom := range []string{"errors", "weird"} {
		got := renderOnCallRunbook(t, map[string]string{"symptom": symptom, "time": "1h", "service": "checkout"})
		if novelty, filter := strings.Index(got, "get_emerging_log_patterns"), strings.Index(got, rule); filter < novelty {
			t.Errorf("symptom %q render must scope log novelty to error severities per severity_set:\n%s", symptom, got)
		}
	}
}

func TestOnCallRunbookLogNoveltyWithoutServiceTargetsTheTriagedService(t *testing.T) {
	got := renderOnCallRunbook(t, map[string]string{"symptom": "errors", "time": "1h", "env": "prod"})
	if !strings.Contains(got, "get_emerging_log_patterns(service_name=<service>, env=prod)") {
		t.Errorf("no-service render must scope novelty to the service fleet triage picked:\n%s", got)
	}
}
