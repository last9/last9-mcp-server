package main

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/models"
	"last9-mcp/internal/workflows"

	last9mcp "github.com/last9/mcp-go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func dashboardPromptSession(t *testing.T) (*mcp.ClientSession, context.Context) {
	t.Helper()
	server, err := last9mcp.NewServerWithOptions("last9-mcp", Version, last9mcp.WithSkipProviderInit())
	if err != nil {
		t.Fatal(err)
	}
	workflows.Register(server)
	registerReferenceResources(server)
	if _, err := registerAllTools(server, models.Config{TokenManager: &auth.TokenManager{}}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	st, ct := mcp.NewInMemoryTransports()
	ss, err := server.Server.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	session, err := mcp.NewClient(&mcp.Implementation{Name: "dashboard-prompts-test", Version: "test"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session, ctx
}

func TestDashboardAuthoringPrompts(t *testing.T) {
	session, ctx := dashboardPromptSession(t)
	listed := map[string]bool{}
	for p, err := range session.Prompts(ctx, nil) {
		if err != nil {
			t.Fatal(err)
		}
		listed[p.Name] = true
	}
	tools := map[string]bool{}
	for tool, err := range session.Tools(ctx, nil) {
		if err != nil {
			t.Fatal(err)
		}
		tools[tool.Name] = true
	}
	for _, name := range []string{"create_dashboard_guide", "dashboard_examples", "query_guide"} {
		t.Run(name, func(t *testing.T) {
			if !listed[name] {
				t.Fatalf("prompts/list missing %s", name)
			}
			result, err := session.GetPrompt(ctx, &mcp.GetPromptParams{Name: name})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Messages) != 1 || result.Messages[0].Role != "user" {
				t.Fatalf("unexpected prompt messages: %#v", result.Messages)
			}
			body, ok := result.Messages[0].Content.(*mcp.TextContent)
			if !ok || body.Text == "" {
				t.Fatal("expected nonempty text")
			}
			refs := regexp.MustCompile("`((?:get|list|prometheus|validate|create|patch|update)_[a-z_]+)`").FindAllStringSubmatch(body.Text, -1)
			if len(refs) == 0 {
				t.Fatal("no tool references checked")
			}
			for _, ref := range refs {
				if !tools[ref[1]] && !listed[ref[1]] {
					t.Errorf("references unavailable tool or prompt %q", ref[1])
				}
			}
			resource, err := session.ReadResource(ctx, &mcp.ReadResourceParams{URI: "last9://reference/" + name})
			if err != nil {
				t.Fatal(err)
			}
			if len(resource.Contents) != 1 || resource.Contents[0].Text != body.Text {
				t.Fatal("resource and prompt must share their source")
			}
			if name == "query_guide" {
				for _, phrase := range []string{"instant", "range", "rate window", "step", "0–100", "trace_*", "nanoseconds", "window_aggregate", "unsupported_execution"} {
					if !strings.Contains(body.Text, phrase) {
						t.Errorf("missing query rule %q", phrase)
					}
				}
			}
			if name == "dashboard_examples" {
				for _, phrase := range []string{"RED per service", "Latency by endpoint", "Errors by status class", "Host/infra health", "Log-count and trace panels", "matches"} {
					if !strings.Contains(body.Text, phrase) {
						t.Errorf("missing example %q", phrase)
					}
				}
			}
			if name == "create_dashboard_guide" {
				previous := -1
				for _, step := range []string{"1. Discover", "2. Lay out", "3. Choose", "4. Build", "5. Validate", "6. Approve", "7. Save", "8. Read back"} {
					index := strings.Index(body.Text, step)
					if index <= previous {
						t.Fatalf("missing or out-of-order step %q", step)
					}
					previous = index
				}
				for _, phrase := range []string{"12-column", "dashboard_definition", "run each panel's query", "`prometheus_range_query`", "`prometheus_instant_query`", "`get_logs`", "`get_traces`", "query_panel", "#475", "unsupported_execution", "`patch_dashboard`", "add_panel", "update_panel"} {
					if !strings.Contains(body.Text, phrase) {
						t.Errorf("missing guidance %q", phrase)
					}
				}
			}
		})
	}
}
