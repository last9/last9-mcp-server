package dashboards

import (
	"fmt"
	"regexp"
	"strconv"
)

// Pure dashboard-variable interpolation.
// Port of agents/supervisor/skills/dashboard_validation/variables.py.

// Bare form requires a leading letter/underscore so label_replace capture
// groups like "$1" are left alone.
var varRefRe = regexp.MustCompile(
	`\$\{(?P<braced>[a-zA-Z_]\w*)\}` +
		`|\$(?P<bare>[a-zA-Z_]\w*)` +
		`|\[\[(?P<bracketed>[a-zA-Z_]\w*)\]\]` +
		`|\{\{(?P<mustache>[a-zA-Z_]\w*)\}\}`,
)

// Grafana built-in variables commonly present in imported dashboards.
var builtinDefaults = map[string]string{
	"__rate_interval": "5m",
	"__interval":      "1m",
	"__interval_ms":   "60000",
	"__range":         "1h",
	"__range_s":       "3600",
	"__range_ms":      "3600000",
}

func matchVarName(m []string) string {
	// Subexp names: braced, bare, bracketed, mustache at indices 1..4
	for i := 1; i < len(m); i++ {
		if m[i] != "" {
			return m[i]
		}
	}
	return ""
}

// dashboardDefaults extracts default values from the dashboard's variables block.
func dashboardDefaults(dashboard map[string]any) map[string]any {
	defaults := map[string]any{}
	rawVars := dashboard["variables"]
	vars, ok := rawVars.([]any)
	if !ok {
		if typed, ok := rawVars.([]map[string]any); ok {
			vars = make([]any, len(typed))
			for i, v := range typed {
				vars[i] = v
			}
		} else {
			return defaults
		}
	}
	for _, raw := range vars {
		v, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		target, _ := v["target"].(string)
		if target == "" {
			target, _ = v["name"].(string)
		}
		if target == "" {
			continue
		}
		var chosen any
		if current := asAnySlice(v["current_values"]); len(current) > 0 {
			chosen = current[0]
		} else if values := asAnySlice(v["values"]); len(values) > 0 {
			chosen = values[0]
		}
		if chosen != nil {
			defaults[target] = chosen
		}
	}
	return defaults
}

// interpolate returns (interpolatedExpr, used, unresolvedNames).
// used maps variable name -> {"value": ..., "source": "caller"|"dashboard_default"|"builtin_default"}.
func interpolate(expr string, callerVars, defaults map[string]any) (string, map[string]map[string]any, []string) {
	used := map[string]map[string]any{}
	var unresolved []string

	resolve := func(name string) (string, bool) {
		var value any
		var source string
		if v, ok := callerVars[name]; ok {
			value, source = v, "caller"
		} else if v, ok := defaults[name]; ok {
			value, source = v, "dashboard_default"
		} else if v, ok := builtinDefaults[name]; ok {
			value, source = v, "builtin_default"
		} else {
			return "", false
		}
		used[name] = map[string]any{"value": value, "source": source}
		return stringifyScalar(value), true
	}

	out := varRefRe.ReplaceAllStringFunc(expr, func(match string) string {
		sub := varRefRe.FindStringSubmatch(match)
		name := matchVarName(sub)
		resolved, ok := resolve(name)
		if !ok {
			for _, u := range unresolved {
				if u == name {
					return match
				}
			}
			unresolved = append(unresolved, name)
			return match
		}
		return resolved
	})
	return out, used, unresolved
}

// interpolatePipeline interpolates every string inside a pipeline structure.
func interpolatePipeline(pipeline any, callerVars, defaults map[string]any) (any, map[string]map[string]any, []string) {
	used := map[string]map[string]any{}
	var unresolved []string

	var walk func(any) any
	walk = func(node any) any {
		switch n := node.(type) {
		case string:
			out, u, unres := interpolate(n, callerVars, defaults)
			for k, v := range u {
				used[k] = v
			}
			for _, name := range unres {
				found := false
				for _, existing := range unresolved {
					if existing == name {
						found = true
						break
					}
				}
				if !found {
					unresolved = append(unresolved, name)
				}
			}
			return out
		case []any:
			out := make([]any, len(n))
			for i, x := range n {
				out[i] = walk(x)
			}
			return out
		case map[string]any:
			out := make(map[string]any, len(n))
			for k, v := range n {
				out[k] = walk(v)
			}
			return out
		default:
			return node
		}
	}
	return walk(pipeline), used, unresolved
}

func stringifyScalar(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case bool:
		return strconv.FormatBool(t)
	case nil:
		return ""
	default:
		return fmt.Sprint(t)
	}
}
