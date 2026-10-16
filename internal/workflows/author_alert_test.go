package workflows

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestAuthorAlertPromptRendersGoal(t *testing.T) {
	result, err := authorAlertHandler(context.Background(), &mcp.GetPromptRequest{
		Params: &mcp.GetPromptParams{
			Arguments: map[string]string{
				"goal":      "p99 latency for checkout above 500ms",
				"entity_id": "entity-abc",
				"signal":    "latency",
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || len(result.Messages) == 0 {
		t.Fatal("expected prompt messages")
	}
	text := ""
	for _, m := range result.Messages {
		if tc, ok := m.Content.(*mcp.TextContent); ok {
			text += tc.Text
		}
	}
	for _, want := range []string{
		"p99 latency for checkout above 500ms",
		"entity-abc",
		"get_entity_kpis",
		"create_kpi",
		"validate_alert_expression",
		"create_alert",
		"patch_alert",
		"last9://reference/alerting",
		"explicit user confirmation",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in:\n%s", want, text)
		}
	}
}
