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

// check is the one interpreter of the input schema: the anyOf branches and allOf
// conditions are evaluated with it too, so every rule reports the same way.
func (s *shapeChecker) check(schema map[string]any, value any, path, kind string) {
	if problem := valueProblem(schema, value); problem != "" {
		s.problems = append(s.problems, path+": "+problem)
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
	if s.requireFields {
		for _, key := range required {
			if _, set := obj[key]; !set && !aliased[key] {
				s.missing(props, key, path)
			}
		}
	}
	s.checkConditions(schema, obj, path)
}

func (s *shapeChecker) missing(props map[string]any, key, path string) {
	propSchema, _ := props[key].(map[string]any)
	s.problems = append(s.problems, fmt.Sprintf("%s.%s: missing; want %s", path, key, describeSchema(propSchema)))
}

// checkConditions reports the allOf if/then and anyOf rules the SDK reports one at a time.
// A failed anyOf names the problems of each branch whose required fields are set, else
// lists the branches; partial panels skip the list and the then requirements.
func (s *shapeChecker) checkConditions(schema, obj map[string]any, path string) {
	allOf, _ := schema["allOf"].([]any)
	for _, c := range allOf {
		cond, _ := c.(map[string]any)
		ifSchema, _ := cond["if"].(map[string]any)
		then, _ := cond["then"].(map[string]any)
		if s.requireFields && ifSchema != nil && then != nil && matches(ifSchema, obj) {
			s.requireFrom(schema, then, obj, path)
		}
	}
	anyOf, _ := schema["anyOf"].([]any)
	options := make([]string, 0, len(anyOf))
	var chosen []string
	for _, b := range anyOf {
		branch, _ := b.(map[string]any)
		sub := shapeChecker{requireFields: true}
		sub.check(branch, obj, path, "")
		if len(sub.problems) == 0 {
			return
		}
		if required, _ := branch["required"].([]string); len(required) > 0 && hasAll(obj, required) {
			chosen = append(chosen, sub.problems...)
		}
		options = append(options, describeBranch(branch, ""))
	}
	if len(chosen) > 0 {
		s.problems = append(s.problems, chosen...)
	} else if s.requireFields && len(options) > 0 {
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

// needsField reports whether obj needs field: it is required, or an anyOf branch requires
// it and no branch is satisfied. An alias for a field obj does not need is dropped harmlessly.
func needsField(schema, obj map[string]any, field string) bool {
	if required, _ := schema["required"].([]string); slices.Contains(required, field) {
		return true
	}
	anyOf, _ := schema["anyOf"].([]any)
	inBranch := false
	for _, b := range anyOf {
		branch, _ := b.(map[string]any)
		if matches(branch, obj) {
			return false
		}
		required, _ := branch["required"].([]string)
		inBranch = inBranch || slices.Contains(required, field)
	}
	return inBranch
}

func matches(schema map[string]any, value any) bool {
	s := shapeChecker{requireFields: true}
	s.check(schema, value, "", "")
	return len(s.problems) == 0
}

func hasAll(obj map[string]any, keys []string) bool {
	for _, key := range keys {
		if _, set := obj[key]; !set {
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

// valueProblem checks the keywords that apply to a single value: type, const, enum and
// numeric bounds.
func valueProblem(schema map[string]any, value any) string {
	if !hasSchemaType(schema, value) {
		return fmt.Sprintf("got %s, want %s", jsonKind(value), describeSchema(schema))
	}
	if c, ok := schema["const"]; ok && value != c {
		return fmt.Sprintf("got %s, want %v", describeValue(value), c)
	}
	if enum, ok := schema["enum"].([]string); ok {
		if str, _ := value.(string); !slices.Contains(enum, str) {
			return fmt.Sprintf("got %s, want %s", describeValue(value), describeSchema(schema))
		}
	}
	n, ok := value.(float64)
	if !ok {
		return ""
	}
	lo, hasLo := schema["minimum"].(int)
	hi, hasHi := schema["maximum"].(int)
	switch {
	case hasLo && hasHi && (n < float64(lo) || n > float64(hi)):
		return fmt.Sprintf("got %v, want %s from %d to %d", n, schema["type"], lo, hi)
	case hasLo && n < float64(lo):
		return fmt.Sprintf("got %v, want %s of at least %d", n, schema["type"], lo)
	case hasHi && n > float64(hi):
		return fmt.Sprintf("got %v, want %s of at most %d", n, schema["type"], hi)
	}
	return ""
}

func describeValue(value any) string {
	if str, ok := value.(string); ok {
		return fmt.Sprintf("%q", str)
	}
	return jsonKind(value)
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
