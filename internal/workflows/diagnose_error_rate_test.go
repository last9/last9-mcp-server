package workflows

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestDiagnoseErrorRateMetadata(t *testing.T) {
	w := DiagnoseErrorRate
	if w.Name != "diagnose-error-rate" {
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

func TestDiagnoseErrorRateMissingRequired(t *testing.T) {
	_, err := DiagnoseErrorRate.Handler(context.Background(), &mcp.GetPromptRequest{
		Params: &mcp.GetPromptParams{Arguments: map[string]string{"service": "checkout"}},
	})
	if err == nil {
		t.Fatal("expected error when 'time' missing")
	}
}

func TestDiagnoseErrorRateAggregatesFirst(t *testing.T) {
	res, err := DiagnoseErrorRate.Handler(context.Background(), &mcp.GetPromptRequest{
		Params: &mcp.GetPromptParams{Arguments: map[string]string{"service": "checkout", "time": "1h"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := res.Messages[0].Content.(*mcp.TextContent).Text
	if !strings.Contains(got, "AGGREGATE FIRST") {
		t.Errorf("template must instruct aggregate-first:\n%s", got)
	}
}
