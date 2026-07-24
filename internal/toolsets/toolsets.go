// Package toolsets implements named MCP tool packs that hard-filter tools/list.
package toolsets

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

// Named toolset → tool membership. "all" and "investigate" are composites
// handled in Parse, not listed here as flat membership.
var named = map[string][]string{
	"logs": {
		"get_logs",
		"get_service_logs",
		"get_log_attributes",
		"get_log_attributes_for_pipeline",
		"get_exceptions",
	},
	"traces": {
		"get_traces",
		"get_service_traces",
		"get_trace_attributes",
		"get_trace_attributes_for_pipeline",
		"get_trace_attribute_values",
	},
	"metrics": {
		"prometheus_range_query",
		"prometheus_instant_query",
		"prometheus_label_values",
		"prometheus_labels",
		"get_service_summary",
		"get_apm_service_deviations",
		"get_service_environments",
		"get_service_performance_details",
		"get_service_operations_summary",
		"get_service_dependency_graph",
		"get_change_events",
		"get_databases",
		"get_database_slow_queries",
		"get_database_queries",
		"get_database_server_metrics",
	},
	"alerts": {
		"get_alerts",
		"get_alert_config",
		"get_entity_alert_rules",
		"get_alert_rule_state",
		"get_notification_channels",
		"get_drop_rules",
		"add_drop_rule",
	},
	"dashboards": {
		"list_dashboards",
		"get_dashboard",
		"create_dashboard",
		"update_dashboard",
		"delete_dashboard",
		"list_dashboard_snapshots",
		"get_dashboard_snapshot",
		"delete_dashboard_snapshot",
	},
}

// discovery tools included in the investigate composite (R9a).
var investigateExtras = []string{
	"did_you_mean",
	"list_datasources",
}

// ValidNames returns sorted valid toolset tokens for error messages.
func ValidNames() []string {
	names := make([]string, 0, len(named)+2)
	for n := range named {
		names = append(names, n)
	}
	names = append(names, "all", "investigate")
	sort.Strings(names)
	return names
}

// Set is the expanded set of allowed tool names. nil means all tools (no filter).
type Set map[string]struct{}

// Allows reports whether toolName may be registered. A nil Set allows everything.
func (s Set) Allows(toolName string) bool {
	if s == nil {
		return true
	}
	_, ok := s[toolName]
	return ok
}

// Parse expands a comma-separated toolset spec into allowed tool names.
// Empty / whitespace-only → nil (all tools). "all" supersedes other tokens.
// Unknown names return an error listing ValidNames.
func Parse(spec string) (Set, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, nil
	}

	parts := strings.Split(spec, ",")
	tokens := make([]string, 0, len(parts))
	for _, p := range parts {
		t := strings.TrimSpace(p)
		if t == "" {
			continue
		}
		tokens = append(tokens, strings.ToLower(t))
	}
	if len(tokens) == 0 {
		return nil, nil
	}

	for _, t := range tokens {
		if t == "all" {
			return nil, nil
		}
	}

	out := make(Set)
	for _, t := range tokens {
		switch t {
		case "investigate":
			for _, domain := range []string{"logs", "traces", "metrics"} {
				for _, tool := range named[domain] {
					out[tool] = struct{}{}
				}
			}
			for _, tool := range investigateExtras {
				out[tool] = struct{}{}
			}
		default:
			tools, ok := named[t]
			if !ok {
				return nil, fmt.Errorf("unknown toolset %q; valid names: %s", t, strings.Join(ValidNames(), ", "))
			}
			for _, tool := range tools {
				out[tool] = struct{}{}
			}
		}
	}
	return out, nil
}

// SpecFromEnv returns the toolsets spec from environment variables.
// Prefer LAST9_TOOLSETS (ff convention); fall back to LAST9_MCP_TOOLSETS.
func SpecFromEnv() string {
	if v := os.Getenv("LAST9_TOOLSETS"); v != "" {
		return v
	}
	return os.Getenv("LAST9_MCP_TOOLSETS")
}
