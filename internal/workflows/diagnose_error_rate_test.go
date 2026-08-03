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
	// env present: step 1 is the single perf-details call, no env discovery.
	withStep1 := lineStartingWith(t, got, "1.")
	if strings.Contains(withStep1, "get_service_environments") {
		t.Errorf("env-present step 1 should not resolve env:\n%s", withStep1)
	}
	without, err := DiagnoseErrorRate.Handler(context.Background(), &mcp.GetPromptRequest{
		Params: &mcp.GetPromptParams{Arguments: map[string]string{"service": "checkout", "time": "1h"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got2 := without.Messages[0].Content.(*mcp.TextContent).Text
	if !strings.Contains(got2, "Resolve env first") {
		t.Errorf("env-absent render should trigger discovery branch:\n%s", got2)
	}
	// env absent: both the discovery call and the perf-details call must stay
	// inside the one numbered step, or markdown collapses the second into an
	// unnumbered continuation and the sequence stops being unambiguous.
	step1 := lineStartingWith(t, got2, "1.")
	if !strings.Contains(step1, "get_service_environments") || !strings.Contains(step1, "get_service_performance_details") {
		t.Errorf("env-absent step 1 must sequence both calls in one list item:\n%s", got2)
	}
}
