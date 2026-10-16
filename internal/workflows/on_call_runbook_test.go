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

// Latency and error triage check a recent change with analyze_change_impact
// right after the first performance read; other branches never call it.
func TestOnCallRunbookChecksRecentChangeOnLatencyAndErrors(t *testing.T) {
	for symptom, wantCall := range map[string]bool{"latency": true, "errors": true, "database": false, "weird": false} {
		res, err := OnCallRunbook.Handler(context.Background(), &mcp.GetPromptRequest{
			Params: &mcp.GetPromptParams{Arguments: map[string]string{"symptom": symptom, "time": "1h", "service": "checkout"}},
		})
		if err != nil {
			t.Fatalf("symptom %q: %v", symptom, err)
		}
		got := res.Messages[0].Content.(*mcp.TextContent).Text
		at := strings.Index(got, "analyze_change_impact")
		if (at >= 0) != wantCall {
			t.Fatalf("symptom %q: analyze_change_impact rendered = %v, want %v:\n%s", symptom, at >= 0, wantCall, got)
		}
		if !wantCall {
			continue
		}
		perf := strings.Index(got, "get_service_performance_details")
		events := strings.Index(got, "get_change_events")
		if !(perf < events && events < at) {
			t.Fatalf("symptom %q: want get_service_performance_details, then get_change_events, then analyze_change_impact:\n%s", symptom, got)
		}
		if !strings.Contains(got, "no significant change") {
			t.Fatalf("symptom %q: the prompt must say how to read a no-change verdict:\n%s", symptom, got)
		}
	}
}
