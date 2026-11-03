package dashboards

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
)

const maxShapeProblems = 20

// fieldAliases maps keys from other dashboard formats (mostly Grafana) to the native
// field they stand for. The API drops unknown keys, so an alias leaves its field unset.
var fieldAliases = map[string]map[string]string{
	"panel": {"gridPos": "layout", "title": "name", "type": "visualization", "targets": "queries"},
	"query": {"query": "expr", "legendFormat": "legend", "refId": "name", "datasource": "telemetry"},
}

// CheckDashboardArguments lists every field of create_dashboard/update_dashboard
// arguments that misses or mistypes the dashboard schema, so one call reports them all.
// Arguments it cannot read as an object pass through for the input schema to reject.
func CheckDashboardArguments(args json.RawMessage) error {
	var doc map[string]any
	if err := json.Unmarshal(args, &doc); err != nil {
		return nil
	}
	dashboard, ok := doc["dashboard"].(map[string]any)
	if !ok {
		return nil
	}
	s := shapeChecker{requireFields: true}
	s.check(dashboardObjectSchema(""), dashboard, "dashboard", "")
	return s.err()
}

// checkPanelShape flags aliased and mistyped fields of a patch panel. Missing fields
// are not flagged: patch_dashboard has always accepted partial panels.
func checkPanelShape(panel map[string]any, path string) []string {
	s := shapeChecker{}
	s.check(panelObjectSchema(), panel, path, "panel")
	return s.problems
}

type shapeChecker struct {
	requireFields bool
	problems      []string
}

func (s *shapeChecker) err() error {
	if len(s.problems) == 0 {
		return nil
	}
	lines := s.problems
	if len(lines) > maxShapeProblems {
		lines = append(lines[:maxShapeProblems:maxShapeProblems], fmt.Sprintf("and %d more", len(s.problems)-maxShapeProblems))
	}
	return errors.New("dashboard does not match the dashboard schema; nothing was written:\n- " + strings.Join(lines, "\n- "))
}

func (s *shapeChecker) check(schema map[string]any, value any, path, kind string) {
	if !hasSchemaType(schema, value) {
		s.problems = append(s.problems, fmt.Sprintf("%s: got %s, want %s", path, jsonKind(value), describeSchema(schema)))
		return
	}
	if items, ok := schema["items"].(map[string]any); ok {
		for i, item := range value.([]any) {
			s.check(items, item, fmt.Sprintf("%s[%d]", path, i), kind)
		}
		return
	}
	obj, ok := value.(map[string]any)
	if !ok {
		return
	}
	props, _ := schema["properties"].(map[string]any)
	required, _ := schema["required"].([]string)
	aliases := fieldAliases[kind]
	aliased := map[string]bool{}
	for _, key := range sortedKeys(obj) {
		if propSchema, ok := props[key].(map[string]any); ok {
			s.check(propSchema, obj[key], path+"."+key, childKind(key))
			continue
		}
		if native, ok := aliases[key]; ok {
			if _, set := obj[native]; !set && needsField(schema, obj, native) {
				aliased[native] = true
				s.problems = append(s.problems, fmt.Sprintf("%s.%s: unknown %s field; use %s", path, key, kind, describeField(props, native)))
			}
		}
	}
	if !s.requireFields {
		return
	}
	for _, key := range required {
		if _, set := obj[key]; !set && !aliased[key] {
			s.missing(props, key, path)
		}
	}
	s.checkConditions(schema, obj, path)
}

func (s *shapeChecker) missing(props map[string]any, key, path string) {
	propSchema, _ := props[key].(map[string]any)
	s.problems = append(s.problems, fmt.Sprintf("%s.%s: missing; want %s", path, key, describeSchema(propSchema)))
}

// checkConditions reports what the allOf if/then and anyOf keywords require, which the
// SDK would otherwise report one at a time.
func (s *shapeChecker) checkConditions(schema, obj map[string]any, path string) {
	allOf, _ := schema["allOf"].([]any)
	for _, c := range allOf {
		cond, _ := c.(map[string]any)
		ifSchema, _ := cond["if"].(map[string]any)
		then, _ := cond["then"].(map[string]any)
		if ifSchema != nil && then != nil && matches(ifSchema, obj) {
			s.requireFrom(schema, then, obj, path)
		}
	}
	anyOf, _ := schema["anyOf"].([]any)
	options := make([]string, 0, len(anyOf))
	for _, b := range anyOf {
		branch, _ := b.(map[string]any)
		if matches(branch, obj) {
			return
		}
		options = append(options, describeBranch(branch, ""))
	}
	if len(options) > 0 {
		s.problems = append(s.problems, fmt.Sprintf("%s: want one of %s", path, strings.Join(options, ", ")))
	}
}

func (s *shapeChecker) requireFrom(schema, cond, obj map[string]any, path string) {
	props, _ := schema["properties"].(map[string]any)
	required, _ := cond["required"].([]string)
	for _, key := range required {
		if _, set := obj[key]; !set {
			s.missing(props, key, path)
		}
	}
	condProps, _ := cond["properties"].(map[string]any)
	for _, key := range sortedKeys(condProps) {
		child, ok := obj[key].(map[string]any)
		propSchema, _ := props[key].(map[string]any)
		sub, _ := condProps[key].(map[string]any)
		if ok {
			s.requireFrom(propSchema, sub, child, path+"."+key)
		}
	}
}

// needsField reports whether obj must set key: it is required, or no anyOf branch
// without it fits obj. The API ignores an alias of a field obj does not need.
func needsField(schema, obj map[string]any, key string) bool {
	required, _ := schema["required"].([]string)
	if slices.Contains(required, key) {
		return true
	}
	anyOf, _ := schema["anyOf"].([]any)
	for _, b := range anyOf {
		branch, _ := b.(map[string]any)
		branchRequired, _ := branch["required"].([]string)
		if !slices.Contains(branchRequired, key) && matches(branch, obj) && pinned(branch, obj) {
			return false
		}
	}
	return len(anyOf) > 0
}

// pinned reports whether value sets every property schema constrains, so that
// matches(schema, value) is not true merely because those properties are absent.
func pinned(schema map[string]any, value any) bool {
	obj, ok := value.(map[string]any)
	if !ok {
		return true
	}
	props, _ := schema["properties"].(map[string]any)
	for key, p := range props {
		sub, _ := p.(map[string]any)
		if v, set := obj[key]; !set || !pinned(sub, v) {
			return false
		}
	}
	return true
}

// matches supports only the keywords the input schema's conditions use.
func matches(schema map[string]any, value any) bool {
	if c, ok := schema["const"]; ok && value != c {
		return false
	}
	if enum, ok := schema["enum"].([]string); ok {
		str, _ := value.(string)
		if !slices.Contains(enum, str) {
			return false
		}
	}
	obj, ok := value.(map[string]any)
	if !ok {
		return true
	}
	required, _ := schema["required"].([]string)
	for _, key := range required {
		if _, set := obj[key]; !set {
			return false
		}
	}
	props, _ := schema["properties"].(map[string]any)
	for key, p := range props {
		sub, _ := p.(map[string]any)
		if v, set := obj[key]; set && !matches(sub, v) {
			return false
		}
	}
	return true
}

func describeBranch(schema map[string]any, prefix string) string {
	required, _ := schema["required"].([]string)
	parts := make([]string, 0, len(required))
	for _, key := range required {
		parts = append(parts, prefix+key)
	}
	props, _ := schema["properties"].(map[string]any)
	for _, key := range sortedKeys(props) {
		sub, _ := props[key].(map[string]any)
		if enum, ok := sub["enum"].([]string); ok {
			parts = append(parts, prefix+key+" "+strings.Join(enum, " or "))
		} else if nested := describeBranch(sub, prefix+key+"."); nested != "" {
			parts = append(parts, nested)
		}
	}
	return strings.Join(parts, " and ")
}

func childKind(key string) string {
	switch key {
	case "panels":
		return "panel"
	case "queries":
		return "query"
	}
	return ""
}

func hasSchemaType(schema map[string]any, value any) bool {
	switch schema["type"] {
	case "object":
		_, ok := value.(map[string]any)
		return ok
	case "array":
		_, ok := value.([]any)
		return ok
	case "string":
		_, ok := value.(string)
		return ok
	case "number":
		_, ok := value.(float64)
		return ok
	case "integer":
		n, ok := value.(float64)
		return ok && n == math.Trunc(n)
	case "boolean":
		_, ok := value.(bool)
		return ok
	}
	return true
}

func jsonKind(value any) string {
	switch value.(type) {
	case nil:
		return "null"
	case map[string]any:
		return "object"
	case []any:
		return "array"
	case string:
		return "string"
	case float64:
		return "number"
	case bool:
		return "boolean"
	}
	return fmt.Sprintf("%T", value)
}

func describeField(props map[string]any, name string) string {
	schema, _ := props[name].(map[string]any)
	if schema["type"] == "object" {
		return name + " " + strings.TrimPrefix(describeSchema(schema), "object ")
	}
	return name
}

func describeSchema(schema map[string]any) string {
	if enum, ok := schema["enum"].([]string); ok {
		return "one of " + strings.Join(enum, ", ")
	}
	t, _ := schema["type"].(string)
	required, _ := schema["required"].([]string)
	if t == "object" && len(required) > 0 {
		return "object {" + strings.Join(required, ", ") + "}"
	}
	if t == "" {
		return "a value"
	}
	return t
}

func sortedKeys(obj map[string]any) []string {
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
