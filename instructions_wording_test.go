package main

import (
	"regexp"
	"slices"
	"strings"
	"testing"

	"last9-mcp/internal/prompts"
)

// pinnedInstructionLines is the wording of every prose line in
// server_instructions.md, with template actions removed and each {{.}} tool
// list written as <tools>. Rewording a line, even one unrelated to the PR's
// own tool, has repeatedly dropped trace_query eval tq-017 (the model moves
// the time window into the pipeline) and log_query lq-mf-005, so the policy
// grows append-only: add lines and append tools to a group freely, but change
// an existing line only together with this list, citing a tq-017 and
// log_query gate run in the PR body.
var pinnedInstructionLines = []string{
	"Last9 observability for the caller's organization. Discover names first, answer with the most specific tool, and treat writes with care.",
	"## Start here",
	"When a request names no service or tool, call <tools> first: it returns environments, signals seen in the last hour, service and alert-group counts, firing alerts and the current time. Otherwise go straight to the most specific tool. A signal it reports absent was only not observed in that hour: still query it for the user's requested time range, index or datasource.",
	"For today's date or a relative window (\"since 9am IST\"), call <tools>.",
	"## Discover before you query",
	"Never guess service, environment, attribute, metric or label names, or dashboard ids; look them up and reuse the exact values.",
	"- Services, environments and datasources: <tools>.",
	"- Log and trace attributes: <tools>.",
	"- Metric and label names: <tools>.",
	"- Log-sending services and their indexes: <tools> over `physical_index_service_count`; no other metric names can be discovered here.",
	"- Alert groups, rules, their KPIs and notification channels: <tools>.",
	"- Dashboards, snapshots and Grafana folders: <tools>; open one by id with <tools>.",
	"## Prefer dedicated tools",
	"They return computed answers with the right scoping; reach for them before writing a query.",
	"- Start a service investigation with <tools>.",
	"- Service health and performance: <tools>.",
	"- Firing alerts and rule state: <tools>.",
	"- Deploys and other changes: <tools>.",
	"- What deviates from baseline: <tools>.",
	"- One service's logs, traces and errors: <tools>.",
	"- Databases: <tools>.",
	"- Profiles: <tools>.",
	"## Raw queries are the last resort",
	"Use <tools> only when no dedicated tool answers the question, and build each query from discovered names.",
	"Put the time window in `start_time_iso`/`end_time_iso` (or `lookback_minutes`), never as `Timestamp` filters in the pipeline.",
	"## Reference manuals",
	"Read these resources before writing a non-trivial query. After a parse stage, reference extracted fields as attributes['name'], never as a bare name:",
	"- `get_logs` pipelines: `last9://reference/logjson`",
	"- `get_traces` pipelines: `last9://reference/tracejson`",
	"- `get_service_logs` filters: `last9://reference/service_logs`",
	"- `prometheus_range_query` PromQL: `last9://reference/metrics`",
	"- Service investigations: `last9://reference/investigation`",
	"## Check before you write",
	"- Dry-run a dashboard with <tools> before saving it.",
	"- Design an alert rule with <tools> before creating it.",
	"- Dry-run an alert expression with <tools> before creating or updating a rule.",
	"- List existing drop rules with <tools> before adding one.",
	"## Write tools",
	"- Create once with <tools> and keep the returned id; never create a second copy to change the first.",
	"- Alert rules from <tools> are created disabled; enable them with <tools> after validating.",
	"- Refine an existing object with <tools>.",
	"- Claim, release or hand off an alert with <tools>; these change ownership, not data.",
	"- Confirm with the user before overwriting or removing data with <tools>.",
	"- <tools> drops matching future logs: confirm with the user first.",
}

func TestServerInstructionsKeepExistingWording(t *testing.T) {
	lines := instructionProseLines(prompts.ServerInstructions)
	for _, want := range pinnedInstructionLines {
		if !slices.Contains(lines, want) {
			t.Errorf("server_instructions.md no longer has the line %q.\n"+
				"Rewording existing lines regresses evals: append a new line instead, or update "+
				"pinnedInstructionLines and cite a tq-017 and log_query gate run in the PR body.", want)
		}
	}
}

func TestInstructionProseLinesIgnoreToolGroups(t *testing.T) {
	const source = "{{- $metrics := tools \"prometheus_labels\" \"get_metric_status\" -}}\n" +
		"{{- with $metrics}}\n" +
		"- Metric and label names: {{.}}{{with tools \"x\"}}; or {{.}}{{end}}.\n" +
		"{{- end}}\n"
	want := []string{"- Metric and label names: <tools>; or <tools>."}
	if got := instructionProseLines(source); !slices.Equal(got, want) {
		t.Errorf("instructionProseLines = %q, want %q", got, want)
	}
}

var (
	toolListAction = regexp.MustCompile(`{{-?\s*\.\s*-?}}`)
	templateAction = regexp.MustCompile(`{{.*?}}`)
)

// instructionProseLines returns the non-empty lines of the instructions
// template with tool lists replaced by <tools> and other actions removed, so
// appending a tool to a group leaves every line unchanged.
func instructionProseLines(source string) []string {
	var lines []string
	for _, line := range strings.Split(source, "\n") {
		line = toolListAction.ReplaceAllString(line, "<tools>")
		if prose := strings.TrimSpace(templateAction.ReplaceAllString(line, "")); prose != "" {
			lines = append(lines, prose)
		}
	}
	return lines
}
