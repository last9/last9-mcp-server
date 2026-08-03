package workflows

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"last9-mcp/internal/prompts"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

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

func TestWorkflowMetadata(t *testing.T) {
	cases := []struct {
		w     Workflow
		name  string
		title string
	}{
		{ScopedLogAttributeDiscovery, "scoped-log-attribute-discovery", "Scoped Log Attribute Discovery"},
		{ExceptionLogContinuation, "exception-log-continuation", "Exception Log Continuation"},
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
			t.Errorf("%s: Arguments = %v, want nil (current workflows are parameter-less)", c.name, c.w.Arguments)
		}
	}
}

func TestWorkflowHandlerReturnsEmbeddedText(t *testing.T) {
	if got := handlerText(t, ScopedLogAttributeDiscovery); got != prompts.ScopedLogAttributeDiscoveryWorkflow {
		t.Errorf("scoped-log-attribute-discovery handler text does not match embedded prompt")
	}
	if got := handlerText(t, ExceptionLogContinuation); got != prompts.ExceptionLogContinuationWorkflow {
		t.Errorf("exception-log-continuation handler text does not match embedded prompt")
	}
}

// Guards the routing language inside the workflow prompt bodies.
func TestWorkflowPromptsContainRoutingGuards(t *testing.T) {
	checks := []struct {
		name   string
		text   string
		phrase string
	}{
		{"scoped log attribute discovery", prompts.ScopedLogAttributeDiscoveryWorkflow, "Forbidden tool for this workflow: `get_service_logs`"},
		{"exception log continuation", prompts.ExceptionLogContinuationWorkflow, "Start with `get_exceptions`"},
	}
	for _, c := range checks {
		if !strings.Contains(c.text, c.phrase) {
			t.Errorf("%s workflow prompt missing %q", c.name, c.phrase)
		}
	}
}

// Locks the read-side parameter wiring: handler reads req.Params.Arguments,
// errors when the required arg is missing, renders when present.
func TestParameterizedHandlerReadPath(t *testing.T) {
	demo := func(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		service := ""
		if req.Params != nil {
			service = req.Params.Arguments["service"]
		}
		if strings.TrimSpace(service) == "" {
			return nil, fmt.Errorf("required argument %q is missing", "service")
		}
		return &mcp.GetPromptResult{
			Messages: []*mcp.PromptMessage{
				{Role: "user", Content: &mcp.TextContent{Text: "service=" + service}},
			},
		}, nil
	}

	if _, err := demo(context.Background(), &mcp.GetPromptRequest{Params: &mcp.GetPromptParams{}}); err == nil {
		t.Error("expected error when required argument 'service' is missing, got nil")
	}

	res, err := demo(context.Background(), &mcp.GetPromptRequest{
		Params: &mcp.GetPromptParams{Arguments: map[string]string{"service": "checkout-api"}},
	})
	if err != nil {
		t.Fatalf("unexpected error with argument present: %v", err)
	}
	tc := res.Messages[0].Content.(*mcp.TextContent)
	if tc.Text != "service=checkout-api" {
		t.Errorf("rendered text = %q, want %q", tc.Text, "service=checkout-api")
	}
}
