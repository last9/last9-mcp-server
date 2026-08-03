package workflows

import (
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestRenderWorkflowErrorsOnMissingRequired(t *testing.T) {
	_, err := renderWorkflow("t", "d", "hello {{.service}}", map[string]string{"time": "1h"}, []string{"service", "time"})
	if err == nil {
		t.Fatal("expected error when required arg 'service' is missing, got nil")
	}
	if !strings.Contains(err.Error(), "service") {
		t.Errorf("error %q should name the missing arg 'service'", err.Error())
	}
}

func TestRenderWorkflowErrorsOnBlankRequired(t *testing.T) {
	_, err := renderWorkflow("t", "d", "x", map[string]string{"service": "   "}, []string{"service"})
	if err == nil {
		t.Fatal("expected error when required arg is blank, got nil")
	}
}

func TestRenderWorkflowInterpolatesArgs(t *testing.T) {
	res, err := renderWorkflow("t", "desc", "svc={{.service}} win={{.time}}", map[string]string{"service": "checkout", "time": "1h"}, []string{"service", "time"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Description != "desc" {
		t.Errorf("Description = %q, want %q", res.Description, "desc")
	}
	if len(res.Messages) != 1 {
		t.Fatalf("got %d messages, want 1", len(res.Messages))
	}
	tc, ok := res.Messages[0].Content.(*mcp.TextContent)
	if !ok {
		t.Fatalf("content is not *mcp.TextContent")
	}
	if tc.Text != "svc=checkout win=1h" {
		t.Errorf("rendered = %q, want %q", tc.Text, "svc=checkout win=1h")
	}
}

func TestRenderWorkflowOptionalConditionalBothWays(t *testing.T) {
	tmpl := "{{if .env}}env={{.env}}{{else}}no-env{{end}}"
	with, err := renderWorkflow("t", "d", tmpl, map[string]string{"env": "prod"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := with.Messages[0].Content.(*mcp.TextContent).Text; got != "env=prod" {
		t.Errorf("env present rendered %q, want %q", got, "env=prod")
	}
	without, err := renderWorkflow("t", "d", tmpl, map[string]string{}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := without.Messages[0].Content.(*mcp.TextContent).Text; got != "no-env" {
		t.Errorf("env absent rendered %q, want %q", got, "no-env")
	}
}
