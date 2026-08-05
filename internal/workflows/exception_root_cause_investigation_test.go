package workflows

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestExceptionRootCauseInvestigationIncludesProfileStep(t *testing.T) {
	res, err := ExceptionRootCauseInvestigation.Handler(context.Background(), &mcp.GetPromptRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := res.Messages[0].Content.(*mcp.TextContent).Text
	if !strings.Contains(got, "get_service_profile") {
		t.Errorf("render must include profile bootstrap step:\n%s", got)
	}
	step2 := lineStartingWith(t, got, "2.")
	if !strings.Contains(step2, "get_service_profile") {
		t.Errorf("step 2 must call get_service_profile after get_exceptions:\n%s", step2)
	}
	if strings.Contains(got, "`get_service_logs` or a non-aggregate") {
		t.Errorf("workflow must not offer get_service_logs as an option after exception investigation:\n%s", got)
	}
	if !strings.Contains(got, "never `get_service_logs`") {
		t.Errorf("workflow must prohibit get_service_logs during exception investigation:\n%s", got)
	}
	if !strings.Contains(got, "non-aggregate `get_logs`") {
		t.Errorf("workflow must use non-aggregate get_logs for bounded line reads:\n%s", got)
	}
}
