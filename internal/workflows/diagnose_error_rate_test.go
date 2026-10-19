package workflows

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestDiagnoseErrorRateMetadata(t *testing.T) {
	w := DiagnoseErrorRate
	if w.Name != "diagnose-error-rate" {
		t.Errorf("Name = %q", w.Name)
	}
	if w.Title == "" || w.Description == "" || w.Handler == nil {
		t.Error("Title/Description/Handler must be set")
	}
	req := map[string]bool{}
	for _, a := range w.Arguments {
		if a.Required {
			req[a.Name] = true
		}
	}
	for _, k := range []string{"service", "time"} {
		if !req[k] {
			t.Errorf("arg %q must be declared Required", k)
		}
	}
}

func TestDiagnoseErrorRateMissingRequired(t *testing.T) {
	_, err := DiagnoseErrorRate.Handler(context.Background(), &mcp.GetPromptRequest{
		Params: &mcp.GetPromptParams{Arguments: map[string]string{"service": "checkout"}},
	})
	if err == nil {
		t.Fatal("expected error when 'time' missing")
	}
}

func TestDiagnoseErrorRateAggregatesFirst(t *testing.T) {
	res, err := DiagnoseErrorRate.Handler(context.Background(), &mcp.GetPromptRequest{
		Params: &mcp.GetPromptParams{Arguments: map[string]string{"service": "checkout", "time": "1h"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := res.Messages[0].Content.(*mcp.TextContent).Text
	if !strings.Contains(got, "AGGREGATE FIRST") {
		t.Errorf("template must instruct aggregate-first:\n%s", got)
	}
	if !strings.Contains(got, "do not provide exact exception time bounds") {
		t.Errorf("template must not treat aggregate exception rows as time-bounded samples:\n%s", got)
	}
}

func TestDiagnoseErrorRateRendersEnvBranches(t *testing.T) {
	with, err := DiagnoseErrorRate.Handler(context.Background(), &mcp.GetPromptRequest{
		Params: &mcp.GetPromptParams{Arguments: map[string]string{"service": "checkout", "time": "1h", "env": "prod"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := with.Messages[0].Content.(*mcp.TextContent).Text
	if !strings.Contains(got, `env "prod"`) || !strings.Contains(got, "env=prod") {
		t.Errorf("env-present render missing env interpolation:\n%s", got)
	}
	if !strings.Contains(got, "get_service_profile") {
		t.Errorf("render must include profile bootstrap step:\n%s", got)
	}
	// env present: step 1 is the single perf-details call, no env discovery.
	withStep1 := lineStartingWith(t, got, "1.")
	if strings.Contains(withStep1, "get_service_environments") || strings.Contains(withStep1, "profile.deployment.envs") {
		t.Errorf("env-present step 1 should not resolve env:\n%s", withStep1)
	}
	without, err := DiagnoseErrorRate.Handler(context.Background(), &mcp.GetPromptRequest{
		Params: &mcp.GetPromptParams{Arguments: map[string]string{"service": "checkout", "time": "1h"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got2 := without.Messages[0].Content.(*mcp.TextContent).Text
	if !strings.Contains(got2, "profile.deployment.envs") {
		t.Errorf("env-absent render should pick env from profile:\n%s", got2)
	}
	if !strings.Contains(got2, "signal_shape.level_field") {
		t.Errorf("render must use profile level_field for log aggregate:\n%s", got2)
	}
	// env absent: profile env pick, fallback, and perf-details must stay in one step.
	step1 := lineStartingWith(t, got2, "1.")
	if !strings.Contains(step1, "get_service_environments") || !strings.Contains(step1, "get_service_performance_details") {
		t.Errorf("env-absent step 1 must sequence env resolution and perf-details in one list item:\n%s", got2)
	}
}

// The change check sits right after the performance read, in its own step.
func TestDiagnoseErrorRateChecksRecentChange(t *testing.T) {
	res, err := DiagnoseErrorRate.Handler(context.Background(), &mcp.GetPromptRequest{
		Params: &mcp.GetPromptParams{Arguments: map[string]string{"service": "checkout", "time": "1h", "env": "prod"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := res.Messages[0].Content.(*mcp.TextContent).Text
	assertChecksRecentChange(t, DiagnoseErrorRate.Name, got)
	step2 := lineStartingWith(t, got, "2.")
	if !strings.Contains(step2, "get_change_events (service_name=checkout)") || !strings.Contains(step2, "analyze_change_impact") {
		t.Errorf("step 2 must find the change and judge it:\n%s", step2)
	}
}

func TestDiagnoseErrorRateChecksLogNoveltyBeforeRawLines(t *testing.T) {
	res, err := DiagnoseErrorRate.Handler(context.Background(), &mcp.GetPromptRequest{
		Params: &mcp.GetPromptParams{Arguments: map[string]string{"service": "checkout", "time": "1h", "env": "prod"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := res.Messages[0].Content.(*mcp.TextContent).Text
	for _, want := range []string{
		"get_emerging_log_patterns(service_name=checkout, env=prod",
		// An empty result rules out only changes above both thresholds, so rarer or sample-only changes must still be checked.
		"no log novelty above min_count and change_factor, not unchanged logs",
		"below_min_count_patterns",
		"sample_limit_reached",
		// Error triage compares error templates, but severity_filters drop body-level lines when severity_set is none or partial.
		`severity_filters=["error", "fatal", "critical"] only when severity_set is "all"`,
		// Unfiltered, info and debug churn can fill the sample and top_n and fold error templates into other.
		`When severity_set is none or partial`,
		`body_filters=["error", "exception", "fatal"]`,
		`raise top_n`,
		"get_log_patterns(service_name=checkout, env=prod) over 1h, with the same severity_filters and body_filters rules",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("template should contain %q:\n%s", want, got)
		}
	}
}

func TestDiagnoseErrorRateNamesDominantLogTemplatesBeforeRawLines(t *testing.T) {
	res, err := DiagnoseErrorRate.Handler(context.Background(), &mcp.GetPromptRequest{
		Params: &mcp.GetPromptParams{Arguments: map[string]string{"service": "checkout", "time": "1h", "env": "prod"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := res.Messages[0].Content.(*mcp.TextContent).Text
	emerging := strings.Index(got, "get_emerging_log_patterns(service_name=checkout, env=prod")
	aggregate := strings.Index(got, "AGGREGATE FIRST")
	patterns := strings.Index(got, "get_log_patterns(service_name=checkout, env=prod")
	rawLines := strings.Index(got, "before reading any raw lines")
	if emerging < 0 || aggregate < emerging || patterns < aggregate || rawLines < patterns {
		t.Errorf("want get_emerging_log_patterns, the aggregate, then get_log_patterns, all before raw lines:\n%s", got)
	}
}
