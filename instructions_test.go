package main

import (
	"context"
	"errors"
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

// namedTools returns the registry tools that text wraps in backticks. Matching
// against the registry, not a name-shaped pattern, keeps backticked parameters
// such as `service_name` from counting as tools.
func namedTools(text string, registry map[string]bool) map[string]bool {
	named := map[string]bool{}
	for name := range registry {
		if strings.Contains(text, "`"+name+"`") {
			named[name] = true
		}
	}
	return named
}

func TestNamedToolsMatchesOnlyRegisteredNames(t *testing.T) {
	registry := map[string]bool{"get_logs": true, "logs": true}
	text := "Use `get_logs` or `logs`; reuse `service_name` values, not `get_logs_x`."
	got := namedTools(text, registry)
	want := map[string]bool{"get_logs": true, "logs": true}
	if len(got) != len(want) {
		t.Fatalf("namedTools = %v, want %v", got, want)
	}
	for name := range want {
		if !got[name] {
			t.Errorf("namedTools = %v, want %v", got, want)
		}
	}
}

// A tool registered without a line in server_instructions.md fails here, so
// the policy cannot silently drift from tools.go.
func TestServerInstructionsNameExactlyServedTools(t *testing.T) {
	_, registry := initializeOver(t, "")
	for _, spec := range []string{"", "logs", "traces", "metrics", "alerts", "dashboards", "profiles", "grafana", "investigate", "logs,alerts"} {
		t.Run("toolsets="+spec, func(t *testing.T) {
			instructions, served := initializeOver(t, spec)
			if len(served) == 0 {
				t.Fatalf("toolsets=%q serves no tools", spec)
			}
			named := namedTools(instructions, registry)
			for tool := range named {
				if !served[tool] {
					t.Errorf("instructions name %q, which toolsets=%q does not serve", tool, spec)
				}
			}
			for tool := range served {
				if !named[tool] {
					t.Errorf("instructions never name %q, which toolsets=%q serves", tool, spec)
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

func TestInstructionsMiddleware(t *testing.T) {
	failure := errors.New("handshake failed")
	tests := []struct {
		name   string
		result mcp.Result
		err    error
		want   string
	}{
		{name: "initialize", result: &mcp.InitializeResult{}, want: "policy"},
		{name: "discover", result: &mcp.DiscoverResult{}, want: "policy"},
		{name: "initialize error", result: &mcp.InitializeResult{}, err: failure},
		{name: "discover error", result: &mcp.DiscoverResult{}, err: failure},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			next := func(context.Context, string, mcp.Request) (mcp.Result, error) {
				return tt.result, tt.err
			}
			res, err := instructionsMiddleware("policy")(next)(context.Background(), "", nil)
			if err != tt.err {
				t.Fatalf("err = %v, want %v", err, tt.err)
			}
			var got string
			switch r := res.(type) {
			case *mcp.InitializeResult:
				got = r.Instructions
			case *mcp.DiscoverResult:
				got = r.Instructions
			}
			if got != tt.want {
				t.Errorf("instructions = %q, want %q", got, tt.want)
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
			mentions: []string{"`get_alert_groups`", "`get_alerts`", "`get_notification_channels`", "`get_drop_rules`", "`add_drop_rule`"},
		},
		{
			spec:      "grafana",
			mentions:  []string{"## Discover before you query", "`grafana_search_dashboards`", "`grafana_get_dashboard`", "`grafana_list_datasources`"},
			omissions: []string{"## Write tools", "last9://reference/"},
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

func TestServerInstructionsWriteSectionNamesOnlyWriteTools(t *testing.T) {
	instructions, registry := initializeOver(t, "")
	reads, writes, ok := strings.Cut(instructions, "## Write tools")
	if !ok {
		t.Fatalf("instructions have no write section:\n%s", instructions)
	}
	for tool := range namedTools(writes, registry) {
		if _, ok := writeTools[tool]; !ok {
			t.Errorf("read tool %q listed under write tools:\n%s", tool, instructions)
		}
	}
	for tool := range namedTools(reads, registry) {
		if _, ok := writeTools[tool]; ok {
			t.Errorf("write tool %q listed outside write tools:\n%s", tool, instructions)
		}
	}
}

// sectionBody returns the text under "## heading" until the next "## " line.
func sectionBody(instructions, heading string) string {
	marker := "## " + heading
	_, rest, ok := strings.Cut(instructions, marker)
	if !ok {
		return ""
	}
	if i := strings.Index(rest, "\n## "); i >= 0 {
		rest = rest[:i]
	}
	return rest
}

func TestServerInstructionsPlaceServiceProfileWithDedicatedTools(t *testing.T) {
	instructions, _ := initializeOver(t, "")
	discover := sectionBody(instructions, "Discover before you query")
	dedicated := sectionBody(instructions, "Prefer dedicated tools")
	if strings.Contains(discover, "`get_service_profile`") {
		t.Errorf("get_service_profile must not sit under discovery; it is the per-service pre-investigation tool:\n%s", discover)
	}
	if !strings.Contains(discover, "`get_profile_services`") {
		t.Errorf("get_profile_services must remain the discovery lister:\n%s", discover)
	}
	if !strings.Contains(dedicated, "`get_service_profile`") {
		t.Errorf("get_service_profile must appear under Prefer dedicated tools:\n%s", dedicated)
	}
}

func TestServerInstructionsDropRuleWording(t *testing.T) {
	instructions, _ := initializeOver(t, "alerts")
	writes := sectionBody(instructions, "Write tools")
	if writes == "" {
		t.Fatalf("alerts toolset missing Write tools:\n%s", instructions)
	}
	if strings.Contains(writes, "overwrite or remove") {
		t.Errorf("add_drop_rule must not be described as overwriting or removing data:\n%s", writes)
	}
	if !strings.Contains(writes, "future logs") {
		t.Errorf("add_drop_rule must say it drops future logs:\n%s", writes)
	}
	if strings.Contains(writes, "`add_drop_rule` overwrite") {
		t.Errorf("single-tool write lines must not use bare 'overwrite' after the tool name:\n%s", writes)
	}
}

func TestServerInstructionsLogsPromQLWithoutLabelDiscovery(t *testing.T) {
	instructions, _ := initializeOver(t, "logs")
	if !strings.Contains(instructions, "`prometheus_instant_query`") {
		t.Fatalf("logs toolset must still name prometheus_instant_query:\n%s", instructions)
	}
	if strings.Contains(instructions, "`prometheus_labels`") || strings.Contains(instructions, "`prometheus_label_values`") {
		t.Fatalf("logs toolset must not claim label discovery tools it does not serve:\n%s", instructions)
	}
	raw := sectionBody(instructions, "Raw queries are the last resort")
	if !strings.Contains(raw, "without metric/label discovery") {
		t.Errorf("logs raw section must note PromQL is served without label discovery:\n%s", raw)
	}
}

func TestServerInstructionsManualsAreLookups(t *testing.T) {
	instructions, _ := initializeOver(t, "")
	manuals := sectionBody(instructions, "Reference manuals")
	if manuals == "" {
		t.Fatalf("missing Reference manuals:\n%s", instructions)
	}
	if strings.Contains(manuals, "before writing a non-trivial query") {
		t.Errorf("manuals must be a lookup, not a push into advanced stages:\n%s", manuals)
	}
	if !strings.Contains(manuals, "Look up these resources when unsure") {
		t.Errorf("manuals should invite lookup when unsure:\n%s", manuals)
	}
}

func TestServerInstructionsTracePercentileStageChoice(t *testing.T) {
	instructions, _ := initializeOver(t, "traces")
	if !strings.Contains(instructions, "`aggregate`") {
		t.Errorf("traces instructions must name aggregate for overall percentiles:\n%s", instructions)
	}
	if !strings.Contains(instructions, "window_aggregate") {
		t.Errorf("traces instructions must contrast window_aggregate for rates/over time:\n%s", instructions)
	}
}

// Nil toolsets.Set allows every name, so tools "create_alert" would leak into
// instructions before that tool is registered. Keep this until #190 lands.
func TestServerInstructionsOmitUnregisteredCreateAlert(t *testing.T) {
	instructions, served := initializeOver(t, "")
	if served["create_alert"] {
		t.Fatal("create_alert is registered; add the disabled-draft write line to server_instructions.md")
	}
	if strings.Contains(instructions, "`create_alert`") {
		t.Errorf("instructions name create_alert before it is registered:\n%s", instructions)
	}
}
