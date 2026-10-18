package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/models"
	"last9-mcp/internal/prompts"
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

func TestServerInstructionsStartWithWorkspaceOverview(t *testing.T) {
	for _, spec := range append(toolsets.ValidNames(), "") {
		t.Run("toolsets="+spec, func(t *testing.T) {
			instructions, _ := initializeOver(t, spec)
			_, rest, ok := strings.Cut(instructions, "\n## ")
			if !ok || !strings.HasPrefix(rest, "Start here\nWhen a request names no service or tool, call `get_workspace_overview` first") {
				t.Fatalf("first section must recommend get_workspace_overview:\n%s", instructions)
			}
		})
	}
}

// An unconditional first call cost agentic evals a hop when the request already
// named its service or tool, and pulled agents off that tool.
func TestServerInstructionsSkipOverviewWhenRequestNamesTarget(t *testing.T) {
	instructions, _ := initializeOver(t, "")
	start, _, _ := strings.Cut(instructions, "\n## Discover")
	if !strings.Contains(start, "Otherwise go straight to the most specific tool.") {
		t.Errorf("Start here must send requests that name their target straight to its tool:\n%s", start)
	}
}

// The overview probes only the last hour in the default scope, so an empty
// probe must not steer agents away from a signal's tools for older windows.
func TestOverviewAbsenceKeepsSignalToolsQueryable(t *testing.T) {
	instructions, _ := initializeOver(t, "")
	start, _, _ := strings.Cut(instructions, "\n## Discover")
	_, telemetry, _ := strings.Cut(prompts.GetWorkspaceOverviewDescription, "- `telemetry`")
	telemetry, _, _ = strings.Cut(telemetry, "\n")
	for name, text := range map[string]string{"initialize instructions": start, "overview telemetry field": telemetry} {
		if strings.Contains(strings.ToLower(text), "skip") {
			t.Errorf("%s tells agents to skip a signal after an empty probe:\n%s", name, text)
		}
		if !strings.Contains(text, "requested time range, index or datasource") {
			t.Errorf("%s must keep the signal queryable for the user's requested scope:\n%s", name, text)
		}
	}
}

// Discover before you query is about names the model must look up and reuse
// exactly; a date line there repeated "never guess" beside the attribute rule.
func TestDiscoverSectionListsOnlyNameLookups(t *testing.T) {
	instructions, _ := initializeOver(t, "investigate")
	start, discover, _ := strings.Cut(instructions, "\n## Discover before you query\n")
	discover, _, _ = strings.Cut(discover, "\n## ")
	if strings.Contains(discover, "`get_current_time`") {
		t.Errorf("Discover before you query must list name lookups only, not get_current_time:\n%s", discover)
	}
	if !strings.Contains(start, "`get_current_time`") {
		t.Errorf("Start here must point to get_current_time for dates and relative windows:\n%s", start)
	}
}

// maxInvestigateInstructionsBytes caps the policy automation sessions receive,
// near its size, so growth is a reviewed decision: it precedes every tool
// description, and growing it moved log_query evals. The parsed-field line
// was a reviewed increase, validated on the full log_query suite. Naming
// get_entity_kpis, which investigate serves, added the rest (#336).
const maxInvestigateInstructionsBytes = 2720

func TestInvestigateInstructionsBounded(t *testing.T) {
	instructions, _ := initializeOver(t, "investigate")
	if len(instructions) > maxInvestigateInstructionsBytes {
		t.Errorf("investigate instructions are %d bytes, budget %d", len(instructions), maxInvestigateInstructionsBytes)
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

func renderInstructions(t *testing.T, tools ...string) string {
	t.Helper()
	allowed := toolsets.Set{}
	for _, tool := range tools {
		allowed[tool] = struct{}{}
	}
	instructions, err := serverInstructions(allowed)
	if err != nil {
		t.Fatal(err)
	}
	return instructions
}

func TestServerInstructionsDestructiveLinesMatchEachToolsEffect(t *testing.T) {
	const dropRuleLine = "- `add_drop_rule` drops matching future logs: confirm with the user first."
	tests := []struct {
		name  string
		tools []string
		want  []string
	}{
		{
			name:  "add_drop_rule alone",
			tools: []string{"add_drop_rule"},
			want:  []string{dropRuleLine},
		},
		{
			name:  "one overwriting tool",
			tools: []string{"delete_dashboard"},
			want:  []string{"- Confirm with the user before overwriting or removing data with `delete_dashboard`."},
		},
		{
			name:  "several overwriting tools and add_drop_rule",
			tools: []string{"add_drop_rule", "update_dashboard", "delete_alert"},
			want: []string{
				"- Confirm with the user before overwriting or removing data with `update_dashboard`, `delete_alert`.",
				dropRuleLine,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			instructions := renderInstructions(t, tt.tools...)
			_, writes, _ := strings.Cut(instructions, "## Write tools")
			var got []string
			for _, line := range strings.Split(writes, "\n") {
				if strings.Contains(line, "confirm") || strings.Contains(line, "Confirm") {
					got = append(got, line)
				}
			}
			if strings.Join(got, "\n") != strings.Join(tt.want, "\n") {
				t.Errorf("confirmation lines = %q, want %q:\n%s", got, tt.want, instructions)
			}
		})
	}
}

// The logs toolset serves prometheus_instant_query for the log index inventory
// metric only; it serves no tool that discovers other metric names.
func TestServerInstructionsOfferPrometheusQueriesOnlyWithMetricDiscovery(t *testing.T) {
	const inventoryLine = "- Log-sending services and their indexes: `prometheus_instant_query` over `physical_index_service_count`; no other metric names can be discovered here."
	tests := []struct {
		spec          string
		rawNames      []string
		rawOmits      []string
		wantInventory bool
	}{
		{spec: "logs", rawNames: []string{"get_logs"}, rawOmits: []string{"prometheus_instant_query", "prometheus_range_query"}, wantInventory: true},
		{spec: "logs,traces", rawNames: []string{"get_logs", "get_traces"}, rawOmits: []string{"prometheus_instant_query", "prometheus_range_query"}, wantInventory: true},
		{spec: "metrics", rawNames: []string{"prometheus_range_query", "prometheus_instant_query"}},
		{spec: "logs,metrics", rawNames: []string{"prometheus_range_query", "prometheus_instant_query", "get_logs"}},
		{spec: "investigate", rawNames: []string{"prometheus_range_query", "prometheus_instant_query", "get_logs", "get_traces"}},
	}
	for _, tt := range tests {
		t.Run("toolsets="+tt.spec, func(t *testing.T) {
			instructions, _ := initializeOver(t, tt.spec)
			raw := lineNaming(instructions, tt.rawNames[0])
			if !strings.HasPrefix(raw, "Use ") {
				t.Fatalf("first line naming %q is %q, want the raw-query line:\n%s", tt.rawNames[0], raw, instructions)
			}
			for _, tool := range tt.rawNames {
				if !strings.Contains(raw, "`"+tool+"`") {
					t.Errorf("raw-query line %q does not name %q", raw, tool)
				}
			}
			for _, tool := range tt.rawOmits {
				if strings.Contains(raw, "`"+tool+"`") {
					t.Errorf("raw-query line %q names %q without a metric-name discovery tool", raw, tool)
				}
			}
			if got := strings.Contains(instructions, inventoryLine); got != tt.wantInventory {
				t.Errorf("inventory line present = %v, want %v:\n%s", got, tt.wantInventory, instructions)
			}
		})
	}
}

func TestServerInstructionsRawQueryLineReadsForOneOrManyTools(t *testing.T) {
	tests := []struct {
		name  string
		tools []string
		want  string
	}{
		{name: "one tool", tools: []string{"get_logs"}, want: "Use `get_logs` only when no dedicated tool answers the question, and build each query from discovered names."},
		{name: "several tools", tools: []string{"get_logs", "get_traces"}, want: "Use `get_logs`, `get_traces` only when no dedicated tool answers the question, and build each query from discovered names."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			instructions := renderInstructions(t, tt.tools...)
			if got := lineNaming(instructions, tt.tools[0]); got != tt.want {
				t.Errorf("raw-query line = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestServerInstructionsSayCreateAlertRulesStartDisabled(t *testing.T) {
	tests := []struct {
		name  string
		tools []string
		want  string
	}{
		{name: "with patch_alert", tools: []string{"create_alert", "patch_alert"}, want: "- Alert rules from `create_alert` are created disabled; enable them with `patch_alert` after validating."},
		{name: "without patch_alert", tools: []string{"create_alert"}, want: "- Alert rules from `create_alert` are created disabled."},
		{name: "without create_alert", tools: []string{"patch_alert", "create_alert_from_chart"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			instructions := renderInstructions(t, tt.tools...)
			var got string
			for _, line := range strings.Split(instructions, "\n") {
				if strings.Contains(line, "created disabled") {
					got = line
				}
			}
			if got != tt.want {
				t.Errorf("created-disabled line = %q, want %q:\n%s", got, tt.want, instructions)
			}
		})
	}
}

// With the Start here block, log_query evals referenced parsed fields by bare
// name (rejected by the validator) until this line named the attributes form;
// it is about get_logs pipelines, so other query tools don't carry it.
func TestReferenceManualsNameParsedLogFieldsAsAttributes(t *testing.T) {
	const parsedFields = "Read these resources before writing a non-trivial query. After a parse stage, reference extracted fields as attributes['name'], never as a bare name:"
	tests := []struct {
		name  string
		tools []string
		want  string
	}{
		{name: "with get_logs", tools: []string{"get_logs", "get_traces"}, want: parsedFields},
		{name: "without get_logs", tools: []string{"get_traces", "get_service_logs"}, want: "Read these resources before writing a non-trivial query:"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			instructions := renderInstructions(t, tt.tools...)
			_, manuals, _ := strings.Cut(instructions, "## Reference manuals\n")
			if got, _, _ := strings.Cut(manuals, "\n"); got != tt.want {
				t.Errorf("reference manuals intro = %q, want %q:\n%s", got, tt.want, instructions)
			}
		})
	}
}

// lineNaming returns the first instructions line that names tool in backticks.
func lineNaming(instructions, tool string) string {
	for _, line := range strings.Split(instructions, "\n") {
		if strings.Contains(line, "`"+tool+"`") {
			return line
		}
	}
	return ""
}

func TestServerInstructionsStartInvestigationWithServiceProfile(t *testing.T) {
	tests := []struct {
		spec string
		want string
	}{
		{spec: "", want: "- Start a service investigation with `get_service_profile`."},
		{spec: "logs", want: "- Start a service investigation with `get_service_profile`."},
		{spec: "traces", want: "- Start a service investigation with `get_service_profile`."},
		{spec: "alerts"},
	}
	for _, tt := range tests {
		t.Run("toolsets="+tt.spec, func(t *testing.T) {
			instructions, _ := initializeOver(t, tt.spec)
			if got := lineNaming(instructions, "get_service_profile"); got != tt.want {
				t.Errorf("get_service_profile line = %q, want %q", got, tt.want)
			}
			if discovery, _, _ := strings.Cut(instructions, "## Prefer dedicated tools"); strings.Contains(discovery, "`get_service_profile`") {
				t.Errorf("get_service_profile listed with discovery tools:\n%s", instructions)
			}
		})
	}
}
