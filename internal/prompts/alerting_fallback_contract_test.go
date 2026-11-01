package prompts_test

import (
	"strings"
	"testing"

	"last9-mcp/internal/prompts"
)

// Separate nonempty vectors can have no matching labels; only the composed expression checks the join.
func TestAlertingFallbackValidatesCombinedExpression(t *testing.T) {
	for name, text := range map[string]string{"manual": prompts.AlertingReference, "workflow_control": prompts.AuthorAlertWorkflow} {
		t.Run(name, func(t *testing.T) {
			_, fallback, ok := strings.Cut(text, "data source client not in context")
			if !ok {
				t.Fatal("missing fallback")
			}
			fallback = strings.SplitN(fallback, "\n", 2)[0]
			if !strings.Contains(fallback, "run the expression with each indicator replaced by its KPI's query") {
				t.Fatalf("fallback must validate the composed expression, not just its operands: %s", fallback)
			}
		})
	}
}
