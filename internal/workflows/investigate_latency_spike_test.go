package workflows

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestInvestigateLatencySpikeMetadata(t *testing.T) {
	w := InvestigateLatencySpike
	if w.Name != "investigate-latency-spike" {
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

func TestInvestigateLatencySpikeMissingRequired(t *testing.T) {
	_, err := InvestigateLatencySpike.Handler(context.Background(), &mcp.GetPromptRequest{
		Params: &mcp.GetPromptParams{Arguments: map[string]string{"time": "1h"}},
	})
	if err == nil {
		t.Fatal("expected error when 'service' missing")
	}
}

func TestInvestigateLatencySpikeRendersEnvBranches(t *testing.T) {
	with, err := InvestigateLatencySpike.Handler(context.Background(), &mcp.GetPromptRequest{
		Params: &mcp.GetPromptParams{Arguments: map[string]string{"service": "checkout", "time": "1h", "env": "prod"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := with.Messages[0].Content.(*mcp.TextContent).Text
	if !strings.Contains(got, `env "prod"`) || !strings.Contains(got, "env=prod") {
		t.Errorf("env-present render missing env interpolation:\n%s", got)
	}
	without, err := InvestigateLatencySpike.Handler(context.Background(), &mcp.GetPromptRequest{
		Params: &mcp.GetPromptParams{Arguments: map[string]string{"service": "checkout", "time": "1h"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got2 := without.Messages[0].Content.(*mcp.TextContent).Text
	if !strings.Contains(got2, "Resolve env first") {
		t.Errorf("env-absent render should trigger discovery branch:\n%s", got2)
	}
}
