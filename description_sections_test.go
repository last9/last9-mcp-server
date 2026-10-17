package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"last9-mcp/internal/prompts"
	"last9-mcp/internal/toolsets"
)

// sectionedTools are get_workspace_overview, the recommended first call, and
// the 15 tools eval cases exercise most (tool-name references across
// last9-mcp-evals cases/*.jsonl at 07c82ee). Their descriptions carry the five
// routing sections below. get_current_time is exempt: it routes nowhere.
var sectionedTools = []string{
	"get_workspace_overview",
	"get_logs",
	"get_service_summary",
	"prometheus_range_query",
	"get_service_logs",
	"get_service_traces",
	"get_exceptions",
	"prometheus_label_values",
	"get_apm_service_deviations",
	"get_alerts",
	"get_traces",
	"get_alert_config",
	"prometheus_instant_query",
	"get_alert_rule_state",
	"create_dashboard",
	"list_datasources",
}

var descriptionSections = []string{"When to use", "When not to use", "Pairs well with", "Output shape", "Known traps"}

// splitSections maps each "## " heading in description to the text under it,
// parsed as prompts.TrimRoutes parses it.
func splitSections(description string) (order []string, bodies map[string]string) {
	bodies = map[string]string{}
	for _, section := range prompts.Sections(description) {
		order = append(order, section.Name)
		bodies[section.Name] = section.Body
	}
	return order, bodies
}

// sectionViolations reports missing or misordered sections and routing
// sections that name anything but a served tool: they are where a model looks
// for the next tool to call, so a stale or misspelled name sends it nowhere.
func sectionViolations(description string, served map[string]bool) []string {
	var out []string
	order, bodies := splitSections(description)
	next := 0
	for _, name := range order {
		if next < len(descriptionSections) && name == descriptionSections[next] {
			next++
		}
	}
	if next < len(descriptionSections) {
		out = append(out, "missing or out of order: ## "+descriptionSections[next])
	}
	for _, section := range descriptionSections {
		body, ok := bodies[section]
		if !ok {
			continue
		}
		if strings.TrimSpace(body) == "" {
			out = append(out, "empty section: "+section)
		}
		if !prompts.IsRoutingSection(section) {
			continue
		}
		for _, line := range strings.Split(body, "\n") {
			if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "- ") {
				out = append(out, section+" line is not a \"- \" bullet: "+line)
			}
		}
		names := prompts.RouteNames(body)
		if len(names) == 0 {
			out = append(out, section+" names no tool")
		}
		for _, name := range names {
			if !served[name] {
				out = append(out, section+" names "+name+", which is not a served tool")
			}
		}
	}
	return out
}

func TestTopToolDescriptionsHaveRoutingSections(t *testing.T) {
	tools, _ := servedSurface(t)
	served := map[string]bool{}
	descriptions := map[string]string{}
	for _, tool := range tools {
		served[tool.Name] = true
		descriptions[tool.Name] = tool.Description
	}
	if len(sectionedTools) != 16 {
		t.Fatalf("sectionedTools has %d entries, want 16", len(sectionedTools))
	}
	for _, name := range sectionedTools {
		description, ok := descriptions[name]
		if !ok {
			t.Errorf("%s is not served", name)
			continue
		}
		for _, v := range sectionViolations(description, served) {
			t.Errorf("%s: %s", name, v)
		}
	}
}

// servedDescriptions maps each tool served under spec to its description.
func servedDescriptions(t *testing.T, spec string) map[string]string {
	t.Helper()
	set, err := toolsets.Parse(spec)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := dumpTools(&buf, set); err != nil {
		t.Fatal(err)
	}
	var out struct {
		Tools []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	descriptions := map[string]string{}
	for _, tool := range out.Tools {
		descriptions[tool.Name] = tool.Description
	}
	return descriptions
}

// A tool served under a narrow --toolsets must not route to a tool that set
// leaves out.
func TestRoutingSectionsNameToolsServedAlongside(t *testing.T) {
	for _, spec := range toolsets.ValidNames() {
		if spec == "all" {
			continue
		}
		descriptions := servedDescriptions(t, spec)
		served := map[string]bool{}
		for name := range descriptions {
			served[name] = true
		}
		for name, description := range descriptions {
			for _, v := range sectionViolations(description, served) {
				if strings.Contains(v, "which is not a served tool") {
					t.Errorf("--toolsets=%s %s: %s", spec, name, v)
				}
			}
		}
	}
}

// get_workspace_overview is served under every toolset, so its follow-up
// pointers must be routes TrimRoutes can drop.
func TestWorkspaceOverviewRoutesFollowToolset(t *testing.T) {
	routes := []string{"`get_service_summary`", "`get_alerts`"}
	_, full := splitSections(servedDescriptions(t, "")["get_workspace_overview"])
	logs := servedDescriptions(t, "logs")["get_workspace_overview"]
	for _, route := range routes {
		if !strings.Contains(full["Pairs well with"], route) {
			t.Errorf("full-surface overview description does not route to %s", route)
		}
		if strings.Contains(logs, "- "+route) {
			t.Errorf("--toolsets=logs overview description routes to %s, which logs does not serve", route)
		}
	}
}

func TestSectionViolationsCatchBadFixtures(t *testing.T) {
	served := map[string]bool{"get_logs": true, "get_traces": true}
	good := "Intro.\n\n## When to use\nx\n\n## When not to use\n- Use `get_traces`\n\n## Pairs well with\n- `get_logs`\n\n## Output shape\nJSON.\n\n## Known traps\nNone.\n"
	tests := []struct {
		name        string
		description string
		want        string
	}{
		{name: "good", description: good},
		{name: "indented headings", description: strings.ReplaceAll(good, "## ", "\t## ")},
		{name: "missing section", description: strings.Replace(good, "## Known traps\nNone.\n", "", 1), want: "missing or out of order: ## Known traps"},
		{name: "out of order", description: strings.Replace(strings.Replace(good, "## Output shape", "## Tmp", 1), "## Known traps", "## Output shape", 1), want: "missing or out of order: ## Known traps"},
		{name: "unknown tool", description: strings.Replace(good, "`get_traces`", "`get_trace`", 1), want: "When not to use names get_trace, which is not a served tool"},
		{name: "parameter in routing section", description: strings.Replace(good, "- `get_logs`", "- `get_logs` with `limit`", 1), want: "Pairs well with names limit, which is not a served tool"},
		{name: "routing section without tool", description: strings.Replace(good, "Use `get_traces`", "Use something else", 1), want: "When not to use names no tool"},
		{name: "routing prose", description: strings.Replace(good, "- `get_logs`", "`get_logs`.", 1), want: "Pairs well with line is not a \"- \" bullet: `get_logs`."},
		{name: "empty section", description: strings.Replace(good, "JSON.\n", "\n", 1), want: "empty section: Output shape"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sectionViolations(tt.description, served)
			if tt.want == "" {
				if len(got) != 0 {
					t.Fatalf("violations = %v, want none", got)
				}
				return
			}
			if len(got) != 1 || got[0] != tt.want {
				t.Fatalf("violations = %v, want [%s]", got, tt.want)
			}
		})
	}
}
