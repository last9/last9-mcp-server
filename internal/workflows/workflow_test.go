package workflows

import (
	"context"
	"strings"
	"testing"

	"last9-mcp/internal/prompts"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// lineStartingWith returns the one rendered line whose trimmed text begins
// with prefix, failing unless exactly one such line exists. Used to assert a
// step's two tool calls stay in a single numbered list item (no bare,
// unnumbered continuation line that markdown would fold into the same item).
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

// TestWorkflowMetadata covers the two parameter-less workflows. The
// parameterized ones assert their own metadata (including arguments) in their
// per-workflow test files.
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
		{"scoped log attribute discovery", prompts.ScopedLogAttributeDiscoveryWorkflow, "Forbidden tool for this workflow: `get_service_logs`"},
		{"exception root cause investigation", prompts.ExceptionRootCauseInvestigationWorkflow, "AGGREGATE FIRST"},
	}
	for _, c := range checks {
		if !strings.Contains(c.text, c.phrase) {
			t.Errorf("%s workflow prompt missing %q", c.name, c.phrase)
		}
	}
}
