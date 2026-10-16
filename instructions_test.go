package main

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/models"
	"last9-mcp/internal/toolsets"

	last9mcp "github.com/last9/mcp-go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// initializeOver connects a client to a server built like main's for spec and
// returns the instructions from initialize plus the served tool names.
func initializeOver(t *testing.T, spec string) (string, map[string]bool) {
	t.Helper()
	allowed, err := toolsets.Parse(spec)
	if err != nil {
		t.Fatal(err)
	}
	server, err := last9mcp.NewServerWithOptions("last9-mcp", Version, last9mcp.WithSkipProviderInit())
	if err != nil {
		t.Fatal(err)
	}
	cfg := models.Config{TokenManager: &auth.TokenManager{}, AllowedTools: allowed}
	if err := registerAllTools(server, cfg); err != nil {
		t.Fatal(err)
	}
	if err := registerServerInstructions(server, allowed); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "instructions-test", Version: Version}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	served := map[string]bool{}
	for tool, err := range session.Tools(ctx, nil) {
		if err != nil {
			t.Fatal(err)
		}
		served[tool.Name] = true
	}
	return session.InitializeResult().Instructions, served
}

var mentionedToolName = regexp.MustCompile("`([a-z][a-z0-9]*(?:_[a-z0-9]+)+)`")

func TestServerInstructionsNameOnlyServedTools(t *testing.T) {
	for _, spec := range []string{"", "logs", "traces", "metrics", "alerts", "dashboards", "profiles", "grafana", "investigate", "logs,alerts"} {
		t.Run("toolsets="+spec, func(t *testing.T) {
			instructions, served := initializeOver(t, spec)
			if !strings.Contains(instructions, "Discover") {
				t.Fatalf("initialize sent no tool-selection policy:\n%s", instructions)
			}
			for _, m := range mentionedToolName.FindAllStringSubmatch(instructions, -1) {
				if !served[m[1]] {
					t.Errorf("instructions name %q, which toolsets=%q does not serve", m[1], spec)
				}
			}
			if strings.Contains(instructions, "\n\n\n") {
				t.Errorf("instructions contain a run of blank lines:\n%s", instructions)
			}
			lines := strings.Split(strings.TrimSpace(instructions), "\n")
			for i, line := range lines {
				if !strings.HasPrefix(line, "## ") {
					continue
				}
				next := ""
				for _, l := range lines[i+1:] {
					if strings.TrimSpace(l) != "" {
						next = l
						break
					}
				}
				if next == "" || strings.HasPrefix(next, "## ") {
					t.Errorf("heading %q has no content under toolsets=%q", line, spec)
				}
			}
		})
	}
}

func TestServerInstructionsAdaptToToolsets(t *testing.T) {
	tests := []struct {
		spec      string
		mentions  []string
		omissions []string
	}{
		{
			spec:     "",
			mentions: []string{"`get_service_summary`", "`prometheus_range_query`", "`get_logs`", "`create_dashboard`", "`delete_dashboard`", "`add_drop_rule`", "last9://reference/logjson", "last9://reference/investigation"},
		},
		{
			spec:      "logs",
			mentions:  []string{"`get_service_profile`", "`get_log_attributes`", "`get_service_logs`", "`get_logs`", "last9://reference/logjson", "last9://reference/service_logs"},
			omissions: []string{"## Write tools", "last9://reference/metrics", "last9://reference/tracejson"},
		},
		{
			spec:      "metrics",
			mentions:  []string{"`prometheus_labels`", "`get_service_summary`", "`get_apm_service_deviations`", "`get_change_events`", "`prometheus_range_query`", "last9://reference/metrics"},
			omissions: []string{"## Write tools", "last9://reference/logjson"},
		},
		{
			spec:      "dashboards",
			mentions:  []string{"## Write tools", "`create_dashboard`", "`update_dashboard`", "`delete_dashboard`"},
			omissions: []string{"## Prefer dedicated tools", "last9://reference/"},
		},
		{
			spec:     "alerts",
			mentions: []string{"`get_alert_groups`", "`get_alerts`", "`add_drop_rule`"},
		},
	}
	for _, tt := range tests {
		t.Run("toolsets="+tt.spec, func(t *testing.T) {
			instructions, _ := initializeOver(t, tt.spec)
			for _, want := range tt.mentions {
				if !strings.Contains(instructions, want) {
					t.Errorf("instructions missing %q:\n%s", want, instructions)
				}
			}
			for _, unwanted := range tt.omissions {
				if strings.Contains(instructions, unwanted) {
					t.Errorf("instructions contain %q:\n%s", unwanted, instructions)
				}
			}
		})
	}
}
