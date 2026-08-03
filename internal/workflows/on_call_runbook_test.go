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
		if strings.Contains(got, "fleet triage") {
			t.Errorf("symptom %q with service set should NOT include fleet triage", symptom)
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
	if got := res.Messages[0].Content.(*mcp.TextContent).Text; !strings.Contains(got, "fleet triage") {
		t.Errorf("no-service render should start with fleet triage:\n%s", got)
	}
}
