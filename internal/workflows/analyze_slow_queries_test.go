package workflows

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestAnalyzeSlowQueriesMetadata(t *testing.T) {
	w := AnalyzeSlowQueries
	if w.Name != "analyze-slow-queries" {
		t.Errorf("Name = %q", w.Name)
	}
	if w.Handler == nil || w.Title == "" || w.Description == "" {
		t.Error("Title/Description/Handler must be set")
	}
	required := map[string]bool{}
	optional := map[string]bool{}
	for _, a := range w.Arguments {
		if a.Required {
			required[a.Name] = true
		} else {
			optional[a.Name] = true
		}
	}
	if !required["time"] {
		t.Error("'time' must be Required")
	}
	if required["db_system"] || required["host"] {
		t.Error("'db_system' and 'host' must be optional")
	}
	if !optional["db_system"] || !optional["host"] {
		t.Error("'db_system' and 'host' must be declared as optional args")
	}
}

func TestAnalyzeSlowQueriesMissingTime(t *testing.T) {
	_, err := AnalyzeSlowQueries.Handler(context.Background(), &mcp.GetPromptRequest{
		Params: &mcp.GetPromptParams{Arguments: map[string]string{"db_system": "postgresql"}},
	})
	if err == nil {
		t.Fatal("expected error when 'time' missing")
	}
}

func TestAnalyzeSlowQueriesDbSystemBranches(t *testing.T) {
	withDB, err := AnalyzeSlowQueries.Handler(context.Background(), &mcp.GetPromptRequest{
		Params: &mcp.GetPromptParams{Arguments: map[string]string{"time": "1h", "db_system": "postgresql"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := withDB.Messages[0].Content.(*mcp.TextContent).Text; !strings.Contains(got, "db_system=postgresql") {
		t.Errorf("db_system-present render missing interpolation:\n%s", got)
	}
	noDB, err := AnalyzeSlowQueries.Handler(context.Background(), &mcp.GetPromptRequest{
		Params: &mcp.GetPromptParams{Arguments: map[string]string{"time": "1h"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := noDB.Messages[0].Content.(*mcp.TextContent).Text; !strings.Contains(got, "surveys all database systems") {
		t.Errorf("db_system-absent render should survey all systems:\n%s", got)
	}
}
