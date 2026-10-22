package main

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"

	last9mcp "github.com/last9/mcp-go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// defaultDescriptionBudget caps a served tool description in bytes. Every
// tools/list carries all descriptions, so each byte costs every session.
const defaultDescriptionBudget = 2500

// descriptionBudgetOverrides lists the tools allowed past the default, each
// near its current size so growth is a reviewed decision. Raise an entry in
// the PR that grows the description, and say why. These and servedSizeBudgets
// are the only description size budgets; add none elsewhere.
var descriptionBudgetOverrides = map[string]int{
	"get_apm_service_deviations":      6300,
	"get_exceptions":                  4900,
	"get_service_summary":             4600,
	"get_alert_config":                4600,
	"get_trace_attribute_deviations":  4400,
	"analyze_change_impact":           3600,
	"get_logs":                        3430,
	"create_dashboard":                3500,
	"prometheus_range_query":          3000,
	"create_alert_from_chart":         3000,
	"get_log_attributes_for_pipeline": 2800,
	"get_trace_waterfall":             2700,
	"get_traces":                      2820,
	"get_service_logs":                2700,
	"create_alert":                    2800,
	"get_service_performance_details": 2800,
	"get_alerts":                      2600,
	"get_emerging_log_patterns":       2700,
}

// servedSizeBudgets cap description plus compact input schema JSON for tools
// whose hand-built schema weighs as much as their description.
var servedSizeBudgets = map[string]int{
	"get_logs": 7500,
}

// maxSchemaDepth bounds input schema nesting (root = 1). The dashboard write
// schemas are the deepest; providers truncate or reject deeper ones.
const maxSchemaDepth = 8

var toolNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

var toolNameVerbs = []string{
	"get", "list", "create", "update", "patch", "delete", "add", "validate", "search",
	"describe", "recommend", "acknowledge", "reassign", "mute", "attach", "detach", "analyze",
}

// toolNamespaces prefix a verb-first name with the product it reads.
var toolNamespaces = []string{"grafana_"}

// legacyToolNames predate the verb-first rule. Renaming a served tool breaks
// every client that calls it, so they stay; new tools get no exemption.
var legacyToolNames = map[string]bool{
	"did_you_mean":             true,
	"prometheus_range_query":   true,
	"prometheus_instant_query": true,
	"prometheus_label_values":  true,
	"prometheus_labels":        true,
}

var resourceURIPattern = regexp.MustCompile(`last9://[A-Za-z0-9_./-]*[A-Za-z0-9_]`)

// toolMetadataViolations returns every static metadata rule tool breaks.
// resources is the set of registered resource URIs.
func toolMetadataViolations(tool *mcp.Tool, resources map[string]bool) []string {
	var out []string
	budget := defaultDescriptionBudget
	if b, ok := descriptionBudgetOverrides[tool.Name]; ok {
		budget = b
	}
	if n := len(tool.Description); n > budget {
		out = append(out, fmt.Sprintf("description is %d bytes, budget %d", n, budget))
	}
	if budget, ok := servedSizeBudgets[tool.Name]; ok {
		schema, err := json.Marshal(tool.InputSchema)
		if err != nil {
			out = append(out, fmt.Sprintf("input schema does not encode: %v", err))
		} else if n := len(tool.Description) + len(schema); n > budget {
			out = append(out, fmt.Sprintf("description and input schema are %d bytes, served-size budget %d", n, budget))
		}
	}
	for _, uri := range resourceURIPattern.FindAllString(tool.Description, -1) {
		if !resources[uri] {
			out = append(out, fmt.Sprintf("description names unregistered resource %s", uri))
		}
	}
	schema := schemaAsAny(tool.InputSchema)
	for _, v := range schemaEnumValues(schema) {
		if _, ok := v.(string); !ok {
			out = append(out, fmt.Sprintf("schema enum value %v (%T) is not a string", v, v))
		}
	}
	if d := schemaDepth(schema); d > maxSchemaDepth {
		out = append(out, fmt.Sprintf("schema depth %d exceeds %d", d, maxSchemaDepth))
	}
	if !toolNamePattern.MatchString(tool.Name) {
		out = append(out, fmt.Sprintf("name %q does not match %s", tool.Name, toolNamePattern))
	} else if !legacyToolNames[tool.Name] && !isVerbFirst(tool.Name) {
		out = append(out, fmt.Sprintf("name %q does not start with a verb (%s)", tool.Name, strings.Join(toolNameVerbs, ", ")))
	}
	return out
}

func isVerbFirst(name string) bool {
	for _, ns := range toolNamespaces {
		name = strings.TrimPrefix(name, ns)
	}
	verb, _, _ := strings.Cut(name, "_")
	return slices.Contains(toolNameVerbs, verb)
}

func schemaAsAny(schema any) any {
	if m, ok := schema.(map[string]any); ok {
		return m
	}
	var out any
	raw, _ := json.Marshal(schema)
	_ = json.Unmarshal(raw, &out)
	return out
}

func schemaEnumValues(node any) []any {
	var out []any
	switch n := node.(type) {
	case map[string]any:
		if enum, ok := n["enum"].([]any); ok {
			out = append(out, enum...)
		}
		for key, v := range n {
			if key != "enum" {
				out = append(out, schemaEnumValues(v)...)
			}
		}
	case []any:
		for _, v := range n {
			out = append(out, schemaEnumValues(v)...)
		}
	}
	return out
}

// schemaDepth counts nested subschemas: properties, items and combinators.
func schemaDepth(node any) int {
	s, ok := node.(map[string]any)
	if !ok {
		return 0
	}
	var children []any
	for _, key := range []string{"properties", "patternProperties", "$defs", "definitions"} {
		if m, ok := s[key].(map[string]any); ok {
			for _, child := range m {
				children = append(children, child)
			}
		}
	}
	for _, key := range []string{"items", "additionalProperties", "not", "contains"} {
		if child, ok := s[key].(map[string]any); ok {
			children = append(children, child)
		}
	}
	for _, key := range []string{"anyOf", "oneOf", "allOf", "prefixItems"} {
		if list, ok := s[key].([]any); ok {
			children = append(children, list...)
		}
	}
	deepest := 0
	for _, child := range children {
		deepest = max(deepest, schemaDepth(child))
	}
	return 1 + deepest
}

// servedSurface lists the tools and resource URIs of a server registered with
// every toolset, exactly as a client sees them.
func servedSurface(t *testing.T) ([]*mcp.Tool, map[string]bool) {
	t.Helper()
	server, err := last9mcp.NewServerWithOptions("last9-mcp-test", "test", last9mcp.WithSkipProviderInit())
	if err != nil {
		t.Fatal(err)
	}
	registerReferenceResources(server)
	if _, err := registerAllTools(server, testToolRegistrationConfig()); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	if _, err := server.Server.Connect(ctx, serverTransport, nil); err != nil {
		t.Fatal(err)
	}
	session, err := mcp.NewClient(&mcp.Implementation{Name: "metadata-test", Version: "test"}, nil).Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })

	var tools []*mcp.Tool
	for tool, err := range session.Tools(ctx, nil) {
		if err != nil {
			t.Fatal(err)
		}
		tools = append(tools, tool)
	}
	resources := map[string]bool{}
	for res, err := range session.Resources(ctx, nil) {
		if err != nil {
			t.Fatal(err)
		}
		resources[res.URI] = true
	}
	return tools, resources
}

func TestServedToolMetadata(t *testing.T) {
	tools, resources := servedSurface(t)
	if len(tools) == 0 || len(resources) == 0 {
		t.Fatalf("served %d tools and %d resources", len(tools), len(resources))
	}
	served := map[string]bool{}
	for _, tool := range tools {
		served[tool.Name] = true
		for _, v := range toolMetadataViolations(tool, resources) {
			t.Errorf("%s: %s", tool.Name, v)
		}
	}
	for name := range descriptionBudgetOverrides {
		if !served[name] {
			t.Errorf("descriptionBudgetOverrides names %q, which is not served", name)
		}
	}
	for name := range servedSizeBudgets {
		if !served[name] {
			t.Errorf("servedSizeBudgets names %q, which is not served", name)
		}
	}
	for name := range legacyToolNames {
		if !served[name] {
			t.Errorf("legacyToolNames names %q, which is not served", name)
		}
	}
}

// Each fixture breaks exactly one rule, so a check that stops firing fails here.
func TestToolMetadataViolationsCatchBadFixtures(t *testing.T) {
	resources := map[string]bool{"last9://reference/logjson": true}
	okSchema := map[string]any{"type": "object", "properties": map[string]any{
		"mode": map[string]any{"type": "string", "enum": []any{"a", "b"}},
	}}
	nested := map[string]any{"type": "string"}
	for range maxSchemaDepth {
		nested = map[string]any{"type": "object", "properties": map[string]any{"x": nested}}
	}
	tests := []struct {
		name string
		tool *mcp.Tool
		want string
	}{
		{name: "clean", tool: &mcp.Tool{Name: "get_fixture", Description: "Reads `last9://reference/logjson`.", InputSchema: okSchema}},
		{name: "legacy name", tool: &mcp.Tool{Name: "prometheus_labels", Description: "ok", InputSchema: okSchema}},
		{name: "namespaced verb", tool: &mcp.Tool{Name: "grafana_list_fixtures", Description: "ok", InputSchema: okSchema}},
		{name: "over default budget", tool: &mcp.Tool{Name: "get_fixture", Description: strings.Repeat("x", defaultDescriptionBudget+1), InputSchema: okSchema}, want: "budget 2500"},
		{name: "over override budget", tool: &mcp.Tool{Name: "get_logs", Description: strings.Repeat("x", descriptionBudgetOverrides["get_logs"]+1), InputSchema: okSchema}, want: "budget 3430"},
		{name: "over served-size budget", tool: &mcp.Tool{Name: "get_logs", Description: "ok", InputSchema: map[string]any{"description": strings.Repeat("x", servedSizeBudgets["get_logs"])}}, want: "served-size budget 7500"},
		{name: "unregistered resource", tool: &mcp.Tool{Name: "get_fixture", Description: "See last9://reference/nope.", InputSchema: okSchema}, want: "unregistered resource last9://reference/nope"},
		{name: "integer enum", tool: &mcp.Tool{Name: "get_fixture", Description: "ok", InputSchema: map[string]any{"type": "object", "properties": map[string]any{
			"version": map[string]any{"type": "integer", "enum": []any{float64(1)}},
		}}}, want: "enum value 1 (float64)"},
		{name: "enum in items", tool: &mcp.Tool{Name: "get_fixture", Description: "ok", InputSchema: map[string]any{"type": "object", "properties": map[string]any{
			"flags": map[string]any{"type": "array", "items": map[string]any{"enum": []any{true}}},
		}}}, want: "enum value true (bool)"},
		{name: "too deep", tool: &mcp.Tool{Name: "get_fixture", Description: "ok", InputSchema: nested}, want: fmt.Sprintf("schema depth %d exceeds %d", maxSchemaDepth+1, maxSchemaDepth)},
		{name: "bad characters", tool: &mcp.Tool{Name: "Get-Fixture", Description: "ok", InputSchema: okSchema}, want: "does not match"},
		{name: "not verb first", tool: &mcp.Tool{Name: "fixture_get", Description: "ok", InputSchema: okSchema}, want: "does not start with a verb"},
		{name: "namespace without verb", tool: &mcp.Tool{Name: "grafana_fixtures", Description: "ok", InputSchema: okSchema}, want: "does not start with a verb"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toolMetadataViolations(tt.tool, resources)
			if tt.want == "" {
				if len(got) != 0 {
					t.Fatalf("violations = %v, want none", got)
				}
				return
			}
			if len(got) != 1 || !strings.Contains(got[0], tt.want) {
				t.Fatalf("violations = %v, want exactly one containing %q", got, tt.want)
			}
		})
	}
}
