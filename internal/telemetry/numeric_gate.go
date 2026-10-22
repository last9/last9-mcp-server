package telemetry

import (
	"fmt"
	"regexp"
	"strings"
)

// numericGatedFunctions read their field through a numeric cast that turns a
// non-number into 0, which silently drags the result down for string fields.
var numericGatedFunctions = map[string]struct{}{
	"$quantile":       {},
	"$quantile_exact": {},
	"$median":         {},
}

var parseNamedCapture = regexp.MustCompile(`\(\?P<([^>]+)>`)

// IsStringBackedField reports whether a field reference reads a string map
// value (attributes['k'] / resources['k']) rather than a typed column.
func IsStringBackedField(field string) bool {
	return strings.HasPrefix(field, "attributes['") || strings.HasPrefix(field, "resources['")
}

// CheckNumericGate makes one ordered pass over sanitized stages. A string-backed
// field used by a gated function must first pass a mandatory canonical numeric
// $regex in a filter/where stage; a later parse or transform that can produce
// the field resets it, so the regex must follow the last of those.
func CheckNumericGate(stages []map[string]interface{}, pathPrefix string) *PipelineArgError {
	safe := map[string]struct{}{}
	for i, stage := range stages {
		stagePath := fmt.Sprintf("%s[%d]", pathPrefix, i)
		switch stageType, _ := stage["type"].(string); stageType {
		case "filter", "where":
			collectCanonicalNumericRegexFields(stage["query"], safe)
		case "parse":
			invalidateParsedFields(stage, safe)
		case "transform":
			transforms, _ := stage["transforms"].([]interface{})
			for _, raw := range transforms {
				entry, _ := raw.(map[string]interface{})
				names, _ := entry["as"].([]interface{})
				for _, name := range names {
					if s, ok := name.(string); ok {
						delete(safe, attributeFieldRef(s))
					}
				}
			}
		case "window_aggregate":
			if function, ok := stage["function"].(map[string]interface{}); ok {
				if e := checkGatedFunction(function, stagePath+".function", safe); e != nil {
					return e
				}
			}
		case "aggregate":
			aggregates, _ := stage["aggregates"].([]interface{})
			for j, raw := range aggregates {
				aggregate, _ := raw.(map[string]interface{})
				function, ok := aggregate["function"].(map[string]interface{})
				if !ok {
					continue
				}
				if e := checkGatedFunction(function, fmt.Sprintf("%s.aggregates[%d].function", stagePath, j), safe); e != nil {
					return e
				}
			}
		}
	}
	return nil
}

func checkGatedFunction(function map[string]interface{}, functionPath string, safe map[string]struct{}) *PipelineArgError {
	for name, raw := range function {
		if _, gated := numericGatedFunctions[strings.TrimSuffix(name, "_over_time")]; !gated {
			continue
		}
		args, _ := raw.([]interface{})
		field, index, ok := AggregateFieldArg(name, args)
		if !ok || !IsStringBackedField(field) {
			continue
		}
		if _, ok := safe[field]; ok {
			continue
		}
		path := fmt.Sprintf("%s.%s[%d]", functionPath, name, index)
		return &PipelineArgError{path, fmt.Sprintf(
			"%s field %q at %s requires a preceding numeric $regex after the last parse that can produce it; use the canonical anchored numeric $regex shown: {\"$regex\":[%q,\"^[0-9]+(?:\\\\.[0-9]+)?$\"]}",
			name, field, path, field,
		)}
	}
	return nil
}

// invalidateParsedFields drops numeric-safe state for fields a parse stage can
// (re)produce: every attribute for label-less json/logfmt, else the declared
// labels and regexp named captures.
func invalidateParsedFields(stage map[string]interface{}, safe map[string]struct{}) {
	parser, _ := stage["parser"].(string)
	labels, labelsDeclared := stage["labels"].(map[string]interface{})

	if (parser == "json" || parser == "logfmt") && (!labelsDeclared || len(labels) == 0) {
		for field := range safe {
			if strings.HasPrefix(field, "attributes['") {
				delete(safe, field)
			}
		}
		return
	}

	for source, rawAlias := range labels {
		delete(safe, attributeFieldRef(source))
		if alias, ok := rawAlias.(string); ok {
			delete(safe, attributeFieldRef(alias))
		}
	}
	if parser == "regexp" {
		pattern, _ := stage["pattern"].(string)
		for _, match := range parseNamedCapture.FindAllStringSubmatch(pattern, -1) {
			delete(safe, attributeFieldRef(match[1]))
		}
	}
}

func attributeFieldRef(name string) string {
	return fmt.Sprintf("attributes['%s']", name)
}

// collectCanonicalNumericRegexFields records only positive canonical regex
// conditions that every matching row must satisfy. A regex beneath $or or $not
// is not a numeric guard; nested $and conditions remain mandatory.
func collectCanonicalNumericRegexFields(node interface{}, fields map[string]struct{}) {
	switch typed := node.(type) {
	case map[string]interface{}:
		for operator, raw := range typed {
			if operator == "$regex" {
				args, ok := raw.([]interface{})
				if !ok || len(args) != 2 {
					continue
				}
				field, fieldOK := args[0].(string)
				pattern, patternOK := args[1].(string)
				if fieldOK && patternOK && isCanonicalNumericRegex(pattern) {
					fields[field] = struct{}{}
				}
				continue
			}
			if operator == "$and" {
				collectCanonicalNumericRegexFields(raw, fields)
			}
		}
	case []interface{}:
		for _, item := range typed {
			collectCanonicalNumericRegexFields(item, fields)
		}
	}
}

func isCanonicalNumericRegex(pattern string) bool {
	pattern = strings.TrimSpace(pattern)
	// Deliberately accept only these canonical anchored shapes. Equivalent exotic
	// forms are rejected so alternation, literals, or broad classes cannot bypass
	// the numeric guard.
	switch pattern {
	case `^[0-9]+$`,
		`^[0-9]+(?:\.[0-9]+)?$`,
		`^-?[0-9]+$`,
		`^-?[0-9]+(?:\.[0-9]+)?$`,
		`^[+-]?[0-9]+$`,
		`^[+-]?[0-9]+(?:\.[0-9]+)?$`:
		return true
	default:
		return false
	}
}
