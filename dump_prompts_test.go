package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"last9-mcp/internal/workflows"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestDumpPromptsListsSixPrompts(t *testing.T) {
	var buf bytes.Buffer
	if err := dumpPrompts(&buf); err != nil {
		t.Fatalf("dumpPrompts error: %v", err)
	}
	var out struct {
		Prompts []struct {
			Name      string `json:"name"`
			Arguments []struct {
				Name     string `json:"name"`
				Required bool   `json:"required"`
			} `json:"arguments"`
		} `json:"prompts"`
	}
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, buf.String())
	}
	names := map[string]bool{}
	for _, p := range out.Prompts {
		names[p.Name] = true
	}
	for _, want := range []string{
		"scoped-log-attribute-discovery", "exception-root-cause-investigation",
		"investigate-latency-spike", "diagnose-error-rate", "analyze-slow-queries", "on-call-runbook",
	} {
		if !names[want] {
			t.Errorf("dump-prompts missing prompt %q", want)
		}
	}
	if len(out.Prompts) < 6 {
		t.Errorf("expected >= 6 prompts, got %d", len(out.Prompts))
	}
	// Prompt names must be unique — AddPrompt is last-wins on a duplicate Name,
	// which would silently drop a workflow.
	seen := map[string]bool{}
	for _, p := range out.Prompts {
		if seen[p.Name] {
			t.Errorf("duplicate prompt name %q", p.Name)
		}
		seen[p.Name] = true
	}
	// Spot-check that a parameterized prompt advertises a required arg.
	for _, p := range out.Prompts {
		if p.Name == "investigate-latency-spike" {
			hasReqService := false
			for _, a := range p.Arguments {
				if a.Name == "service" && a.Required {
					hasReqService = true
				}
			}
			if !hasReqService {
				t.Error("investigate-latency-spike must advertise required arg 'service'")
			}
		}
	}
}

func TestDumpPromptsExceptionWorkflowIncludesProfileStep(t *testing.T) {
	res, err := workflows.ExceptionRootCauseInvestigation.Handler(context.Background(), &mcp.GetPromptRequest{})
	if err != nil {
		t.Fatalf("exception-root-cause-investigation handler: %v", err)
	}
	if len(res.Messages) != 1 {
		t.Fatalf("got %d messages, want 1", len(res.Messages))
	}
	got := res.Messages[0].Content.(*mcp.TextContent).Text
	if !strings.Contains(got, "get_service_profile") {
		t.Errorf("exception-root-cause-investigation prompt must include get_service_profile:\n%s", got)
	}
}
