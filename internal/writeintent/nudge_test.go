package writeintent

import (
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestRefineNudge(t *testing.T) {
	got := RefineNudge(Dashboard, "abc")
	want := "To refine this dashboard, call update_dashboard with id=abc. Do not call create_dashboard again."
	if got != want {
		t.Fatalf("RefineNudge = %q, want %q", got, want)
	}
	if RefineNudge(Dashboard, "") != "" {
		t.Fatal("empty id must not invent a nudge")
	}
}

func TestAnnotateAppendsSecondTextPart(t *testing.T) {
	body := `{"dashboard":{"id":"new-id"}}`
	result := &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: body}},
		Meta:    map[string]any{"reference_url": "/dashboards/new-id"},
	}

	got := Annotate(result, Dashboard, "new-id")
	if got != result {
		t.Fatal("Annotate must return the same result pointer")
	}
	if len(got.Content) != 2 {
		t.Fatalf("Content len %d, want 2", len(got.Content))
	}
	first, ok := got.Content[0].(*mcp.TextContent)
	if !ok || first.Text != body {
		t.Fatalf("Content[0] mutated: %#v", got.Content[0])
	}
	second, ok := got.Content[1].(*mcp.TextContent)
	if !ok || !strings.Contains(second.Text, "update_dashboard") || !strings.Contains(second.Text, "id=new-id") {
		t.Fatalf("Content[1] = %#v", got.Content[1])
	}
	if got.Meta["reference_url"] != "/dashboards/new-id" {
		t.Fatalf("Meta mutated: %#v", got.Meta)
	}
}

func TestAnnotateEmptyIDDoesNotGrowContent(t *testing.T) {
	result := &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: `{"dashboard":{}}`}},
	}
	got := Annotate(result, Dashboard, "")
	if len(got.Content) != 1 {
		t.Fatalf("Content len %d, want 1", len(got.Content))
	}
}

func TestAnnotateNilResult(t *testing.T) {
	if Annotate(nil, Dashboard, "id") != nil {
		t.Fatal("nil result must stay nil")
	}
}
