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
}
