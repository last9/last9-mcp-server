package logs

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
)

// namedCapturePattern extracts (?P<name>…) groups from a regexp parse pattern.
var namedCapturePattern = regexp.MustCompile(`\(\?P<([^>]+)>`)

// validNamedCaptureName is the Go regexp named-group alphabet (no `$`, hyphens, etc.).
var validNamedCaptureName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Validation error categories for fail-closed log pipeline checks.
// Stable strings — safe for metrics labels; never include customer payloads.
const (
	LogValidationUnknownStageType = "unknown_stage_type"
	LogValidationUnknownStageKey  = "unknown_stage_key"
	LogValidationMissingRequired  = "missing_required"
	LogValidationInvalidField     = "invalid_field"
	LogValidationWrongDomainField = "wrong_domain_field"
)

// LogPipelineValidationError is a fail-closed validation failure with a stable
// category + JSON path for metrics and model self-correction.
type LogPipelineValidationError struct {
	Category string
	Path     string
	Message  string
}

func (e *LogPipelineValidationError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

func newLogValidationError(category, path, message string) error {
	return &LogPipelineValidationError{
		Category: category,
		Path:     path,
		Message:  message,
	}
}

// traceOnlyLogFields are span/trace fields that have no meaning as top-level
// log filters. Accepting them silently produces zero results; rejecting them
// surfaces the model error before the API call.
// TraceId, SpanId, ParentSpanId are intentionally excluded — they are valid
// log fields used for log↔trace correlation queries.
var traceOnlyLogFields = map[string]struct{}{
	"SpanKind":   {},
	"StatusCode": {},
	"Duration":   {},
	"SpanName":   {},
}

// validParserValues are the accepted values for the parse stage "parser" key.
var validParserValues = map[string]struct{}{
	"json":   {},
	"logfmt": {},
	"regexp": {},
}

// allowedStageKeys lists the permitted top-level keys for each stage type.
// Any key not in the set is rejected so wrong-name mistakes are caught
// (e.g. window_minutes on window_aggregate, or format on parse).
var allowedStageKeys = map[string]map[string]struct{}{
	"filter": {
		"type":  {},
		"query": {},
	},
	"parse": {
		"type":    {},
		"parser":  {},
		"field":   {},
		"pattern": {},
		"labels":  {},
	},
	"aggregate": {
		"type":       {},
		"aggregates": {},
		"groupby":    {},
	},
	"window_aggregate": {
		"type":     {},
		"function": {},
		"as":       {},
		"window":   {},
		"groupby":  {},
	},
}

// prepareLogJSONQuery validates then sanitizes a logjson pipeline, then
// normalizes bare top-level filter queries into $and (traces parity).
// It is the single entry point for both get_logs and
// get_log_attributes_for_pipeline before any HTTP call is made.
// pathPrefix is used in error messages — "logjson_query" for get_logs,
// "pipeline" for get_log_attributes_for_pipeline — including sanitize failures.
func prepareLogJSONQuery(stages []map[string]interface{}, pathPrefix string) ([]map[string]interface{}, error) {
	if pathPrefix == "" {
		pathPrefix = "logjson_query"
	}

	// Validate structure first, then sanitize field refs (which also defaults
	// missing parse "field" to "Body" on rebuilt copies), then $and-wrap. The
	// ordered semantic pass intentionally runs last so aliases are canonical.
	if err := validateLogJSONQuery(stages, pathPrefix); err != nil {
		return nil, err
	}

	sanitized, err := sanitizeLogJSONQueryPrefixed(stages, pathPrefix)
	if err != nil {
		return nil, err
	}

	for _, stage := range sanitized {
		if stageType, _ := stage["type"].(string); stageType == "filter" {
			if query, ok := stage["query"]; ok {
				stage["query"] = wrapTopLevelLogFilterQuery(query)
			}
		}
	}
	if err := validateQuantileNumericDataflow(sanitized, pathPrefix); err != nil {
		return nil, err
	}

	return sanitized, nil
}

// wrapTopLevelLogFilterQuery ensures the top-level filter query is wrapped in a
// logical operator. Already-$and/$or/$not queries pass through; bare field
// operators are wrapped in $and (deterministic key order).
func wrapTopLevelLogFilterQuery(query interface{}) interface{} {
	queryMap, ok := query.(map[string]interface{})
	if !ok || len(queryMap) == 0 {
		return query
	}

	if len(queryMap) == 1 {
		for key := range queryMap {
			if _, isLogical := logFilterLogicalOperators[key]; isLogical {
				return queryMap
			}
		}
	}

	keys := make([]string, 0, len(queryMap))
	for key := range queryMap {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	conditions := make([]interface{}, 0, len(keys))
	for _, key := range keys {
		conditions = append(conditions, map[string]interface{}{key: queryMap[key]})
	}
	return map[string]interface{}{"$and": conditions}
}

// validateLogJSONQuery performs structural validation before the HTTP fan-out.
// Stage ordering (filter → parse → aggregate/window_aggregate) is documented in
// the whale description and logjson reference but is not enforced here (MVP).
func validateLogJSONQuery(stages []map[string]interface{}, pathPrefix string) error {
	for i, stage := range stages {
		stagePath := fmt.Sprintf("%s[%d]", pathPrefix, i)

		stageType, _ := stage["type"].(string)
		if stageType == "" {
			return newLogValidationError(
				LogValidationMissingRequired,
				stagePath,
				fmt.Sprintf("missing or non-string \"type\" at %s — each stage must set type to filter|parse|aggregate|window_aggregate", stagePath),
			)
		}

		allowed, ok := allowedStageKeys[stageType]
		if !ok {
			return newLogValidationError(
				LogValidationUnknownStageType,
				stagePath,
				fmt.Sprintf("unknown stage type %q at %s — valid types are filter, parse, aggregate, window_aggregate", stageType, stagePath),
			)
		}

		// Reject any key not in the allowlist for this stage type.
		for key := range stage {
			if _, ok := allowed[key]; !ok {
				msg := fmt.Sprintf(
					"unknown key %q on %s stage at %s — allowed keys: %s",
					key, stageType, stagePath, stageKeyList(allowed),
				)
				if stageType == "window_aggregate" {
					msg += `; common mistake: window_aggregate uses "function"+"as"+"window" not "aggregates" or "window_minutes"`
				}
				if stageType == "parse" && key == "format" {
					msg += `; use "parser": "json"|"logfmt"|"regexp" (never "format")`
				}
				if stageType == "filter" && key == "conditions" {
					msg += `; use "query" not "conditions"`
				}
				if stageType == "aggregate" && key == "aggs" {
					msg += `; use "aggregates" (plural), not "aggs" or "aggregations"`
				}
				return newLogValidationError(LogValidationUnknownStageKey, stagePath+"."+key, msg)
			}
		}

		switch stageType {
		case "parse":
			if err := validateParseStage(stage, stagePath); err != nil {
				return err
			}
		case "window_aggregate":
			if err := validateWindowAggregateStage(stage, stagePath); err != nil {
				return err
			}
			if err := validateGroupByForTraceFields(stage, stagePath); err != nil {
				return err
			}
		case "aggregate":
			if err := validateAggregateStage(stage, stagePath); err != nil {
				return err
			}
		case "filter":
			if err := validateFilterStage(stage, stagePath); err != nil {
				return err
			}
		}
	}
	return nil
}

// validateQuantileNumericDataflow makes one ordered pass over the sanitized
// pipeline. String-backed attribute/resource fields become numeric-safe only
// after a mandatory canonical regex. A later parse invalidates fields it can
// produce, so the regex must follow the last relevant parse.
func validateQuantileNumericDataflow(stages []map[string]interface{}, pathPrefix string) error {
	numericSafeFields := make(map[string]struct{})

	for stageIndex, stage := range stages {
		stagePath := fmt.Sprintf("%s[%d]", pathPrefix, stageIndex)
		stageType, _ := stage["type"].(string)

		switch stageType {
		case "filter":
			fields := make(map[string]struct{})
			collectMandatoryCanonicalNumericRegexFields(stage["query"], fields)
			for field := range fields {
				numericSafeFields[field] = struct{}{}
			}
			continue
		case "parse":
			invalidateParsedNumericFields(stage, numericSafeFields)
			continue
		}

		checkFunction := func(function map[string]interface{}, functionPath string) error {
			rawArgs, ok := function["$quantile"].([]interface{})
			if !ok || len(rawArgs) != 2 {
				return nil
			}
			field, _ := rawArgs[1].(string)
			if !isStringBackedLogField(field) {
				return nil
			}
			if _, ok := numericSafeFields[field]; ok {
				return nil
			}
			return newLogValidationError(
				LogValidationInvalidField,
				functionPath+".$quantile[1]",
				fmt.Sprintf(
					"$quantile field %q at %s requires a preceding numeric $regex after the last parse that can produce it; use the canonical anchored numeric $regex shown: {\"$regex\":[%q,\"^[0-9]+(?:\\\\.[0-9]+)?$\"]}",
					field, functionPath+".$quantile[1]", field,
				),
			)
		}

		switch stageType {
		case "window_aggregate":
			if function, ok := stage["function"].(map[string]interface{}); ok {
				if err := checkFunction(function, stagePath+".function"); err != nil {
					return err
				}
			}
		case "aggregate":
			aggregates, _ := stage["aggregates"].([]interface{})
			for aggregateIndex, rawAggregate := range aggregates {
				aggregate, ok := rawAggregate.(map[string]interface{})
				if !ok {
					continue
				}
				function, ok := aggregate["function"].(map[string]interface{})
				if !ok {
					continue
				}
				functionPath := fmt.Sprintf("%s.aggregates[%d].function", stagePath, aggregateIndex)
				if err := checkFunction(function, functionPath); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func isStringBackedLogField(field string) bool {
	return strings.HasPrefix(field, "attributes['") || strings.HasPrefix(field, "resources['")
}

func invalidateParsedNumericFields(stage map[string]interface{}, numericSafeFields map[string]struct{}) {
	parser, _ := stage["parser"].(string)
	labels, labelsDeclared := stage["labels"].(map[string]interface{})

	if (parser == "json" || parser == "logfmt") && (!labelsDeclared || len(labels) == 0) {
		for field := range numericSafeFields {
			if strings.HasPrefix(field, "attributes['") {
				delete(numericSafeFields, field)
			}
		}
		return
	}

	for source, rawAlias := range labels {
		delete(numericSafeFields, attributeFieldRef(source))
		if alias, ok := rawAlias.(string); ok {
			delete(numericSafeFields, attributeFieldRef(alias))
		}
	}
	if parser == "regexp" {
		pattern, _ := stage["pattern"].(string)
		for _, match := range namedCapturePattern.FindAllStringSubmatch(pattern, -1) {
			delete(numericSafeFields, attributeFieldRef(match[1]))
		}
	}
}

func attributeFieldRef(name string) string {
	return fmt.Sprintf("attributes['%s']", name)
}

// collectMandatoryCanonicalNumericRegexFields records only positive canonical
// regex conditions that every matching row must satisfy. A regex beneath $or
// or $not is not a numeric guard; nested $and conditions remain mandatory.
func collectMandatoryCanonicalNumericRegexFields(node interface{}, fields map[string]struct{}) {
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
				collectMandatoryCanonicalNumericRegexFields(raw, fields)
			}
		}
	case []interface{}:
		for _, item := range typed {
			collectMandatoryCanonicalNumericRegexFields(item, fields)
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

func validateParseStage(stage map[string]interface{}, stagePath string) error {
	parser, _ := stage["parser"].(string)
	if parser == "" {
		return newLogValidationError(
			LogValidationMissingRequired,
			stagePath+".parser",
			fmt.Sprintf("parse stage at %s missing required \"parser\" key — use \"parser\": \"json\"|\"logfmt\"|\"regexp\" (never \"format\")", stagePath),
		)
	}
	if _, ok := validParserValues[parser]; !ok {
		return newLogValidationError(
			LogValidationInvalidField,
			stagePath+".parser",
			fmt.Sprintf("invalid parser %q at %s — must be one of: json, logfmt, regexp (never \"format\")", parser, stagePath),
		)
	}

	if labels, ok := stage["labels"].(map[string]interface{}); ok {
		for labelKey := range labels {
			if !safeBodyKeyPattern.MatchString(labelKey) {
				return newLogValidationError(
					LogValidationInvalidField,
					stagePath+".labels."+labelKey,
					fmt.Sprintf(
						"parse labels key %q at %s is invalid — use a safe key (letters/digits/underscore/dot/@/-); never \"$merchant\" or keys with spaces, quotes, or backslashes",
						labelKey, stagePath+".labels",
					),
				)
			}
		}
	}

	if parser == "regexp" {
		pattern, _ := stage["pattern"].(string)
		if strings.TrimSpace(pattern) == "" {
			return newLogValidationError(
				LogValidationMissingRequired,
				stagePath+".pattern",
				fmt.Sprintf("regexp parse stage at %s missing required \"pattern\" with named captures like (?P<level>ERROR|WARN)", stagePath),
			)
		}
		if err := validateRegexpNamedCaptures(pattern, stagePath+".pattern"); err != nil {
			return err
		}
	}
	return nil
}

func validateRegexpNamedCaptures(pattern, path string) error {
	matches := namedCapturePattern.FindAllStringSubmatch(pattern, -1)
	if len(matches) == 0 {
		return newLogValidationError(
			LogValidationInvalidField,
			path,
			fmt.Sprintf("regexp pattern at %s has no named captures — use (?P<name>…) groups (name must match [A-Za-z_][A-Za-z0-9_]*)", path),
		)
	}
	for _, m := range matches {
		name := m[1]
		if !validNamedCaptureName.MatchString(name) {
			return newLogValidationError(
				LogValidationInvalidField,
				path,
				fmt.Sprintf(
					"invalid regexp named capture %q at %s — Go named groups must match [A-Za-z_][A-Za-z0-9_]* (got (?P<%s>…); never include \"$\"). Example: (?P<merchant>\\\\S+)",
					name, path, name,
				),
			)
		}
	}
	return nil
}

func validateGroupByForTraceFields(stage map[string]interface{}, stagePath string) error {
	raw, ok := stage["groupby"]
	if !ok || raw == nil {
		return nil
	}
	groupBy, ok := raw.(map[string]interface{})
	if !ok {
		return newLogValidationError(
			LogValidationInvalidField,
			stagePath+".groupby",
			fmt.Sprintf(
				"groupby at %s must be an object like {\"ServiceName\":\"service\"}, not %T",
				stagePath+".groupby", raw,
			),
		)
	}
	for fieldRef := range groupBy {
		if _, isTraceOnly := traceOnlyLogFields[fieldRef]; isTraceOnly {
			return newLogValidationError(
				LogValidationWrongDomainField,
				stagePath+".groupby."+fieldRef,
				fmt.Sprintf(
					"groupby at %s uses trace-only field %q — not valid in logs; use ServiceName, SeverityText, attributes['…'], or resources['…']",
					stagePath+".groupby", fieldRef,
				),
			)
		}
	}
	return nil
}

func validateWindowAggregateStage(stage map[string]interface{}, stagePath string) error {
	fn := stage["function"]
	if fn == nil {
		return newLogValidationError(
			LogValidationMissingRequired,
			stagePath+".function",
			fmt.Sprintf("window_aggregate at %s missing required \"function\" key — example: {\"function\":{\"$count\":[]},\"as\":\"errors\",\"window\":[\"1\",\"minutes\"]}", stagePath),
		)
	}
	fnMap, ok := fn.(map[string]interface{})
	if !ok {
		return newLogValidationError(
			LogValidationInvalidField,
			stagePath+".function",
			fmt.Sprintf("window_aggregate at %s: \"function\" must be an object like {\"$count\":[]}, not a string — got %T", stagePath, fn),
		)
	}
	if err := validateQuantileFunction(fnMap, stagePath+".function"); err != nil {
		return err
	}

	as, _ := stage["as"].(string)
	if as == "" {
		return newLogValidationError(
			LogValidationMissingRequired,
			stagePath+".as",
			fmt.Sprintf("window_aggregate at %s missing required \"as\" key (output field name) — example: \"as\":\"errors\"", stagePath),
		)
	}

	rawWindow := stage["window"]
	if rawWindow == nil {
		return newLogValidationError(
			LogValidationMissingRequired,
			stagePath+".window",
			fmt.Sprintf("window_aggregate at %s missing required \"window\" key — example: \"window\":[\"1\",\"minutes\"]", stagePath),
		)
	}
	window, ok := rawWindow.([]interface{})
	gotLen := 0
	if ok {
		gotLen = len(window)
	}
	if !ok || gotLen != 2 {
		return newLogValidationError(
			LogValidationInvalidField,
			stagePath+".window",
			fmt.Sprintf("window_aggregate at %s: \"window\" must be an array of exactly 2 elements [duration, unit] — example: [\"1\",\"minutes\"]; got %T len=%d", stagePath, rawWindow, gotLen),
		)
	}
	return nil
}

// validateFilterStageForTraceFields rejects top-level log filter conditions
// that reference trace-only fields (SpanKind, StatusCode, etc.) that are
// meaningless in the log context and always produce zero results.
func validateFilterStageForTraceFields(stage map[string]interface{}, stagePath string) error {
	query, ok := stage["query"].(map[string]interface{})
	if !ok {
		return nil
	}
	return walkFilterForTraceFields(query, stagePath+".query")
}

func walkFilterForTraceFields(node interface{}, path string) error {
	switch typed := node.(type) {
	case map[string]interface{}:
		for key, val := range typed {
			switch key {
			case "$and", "$or", "$not":
				if err := walkFilterForTraceFields(val, path+"."+key); err != nil {
					return err
				}
			default:
				args, ok := val.([]interface{})
				if !ok || len(args) == 0 {
					continue
				}
				fieldRef, _ := args[0].(string)
				if fieldRef == "" {
					continue
				}
				if strings.Contains(fieldRef, "'") || strings.Contains(fieldRef, "[") {
					continue
				}
				if _, isTraceOnly := traceOnlyLogFields[fieldRef]; isTraceOnly {
					return newLogValidationError(
						LogValidationWrongDomainField,
						path+"."+key,
						fmt.Sprintf(
							"log filter at %s references trace-only field %q — this field does not exist in logs and will match nothing; "+
								"use SeverityText for severity, Body for message content, or call get_log_attributes to discover the correct log field",
							path+"."+key, fieldRef,
						),
					)
				}
			}
		}
	case []interface{}:
		for i, item := range typed {
			if err := walkFilterForTraceFields(item, fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
	}
	return nil
}

// validateFilterStage checks that a filter stage has a non-nil "query" key
// that is a condition object (map), then delegates trace-field checks.
func validateFilterStage(stage map[string]interface{}, stagePath string) error {
	if stage["query"] == nil {
		return newLogValidationError(
			LogValidationMissingRequired,
			stagePath+".query",
			fmt.Sprintf(
				"filter stage at %s missing required \"query\" key — use \"query\" not \"conditions\"",
				stagePath,
			),
		)
	}
	if _, ok := stage["query"].(map[string]interface{}); !ok {
		return newLogValidationError(
			LogValidationInvalidField,
			stagePath+".query",
			fmt.Sprintf(
				"filter stage at %s: \"query\" must be a condition object (NOT SQL / not a string) — example: {\"query\":{\"$and\":[{\"$eq\":[\"SeverityText\",\"ERROR\"]}]}}",
				stagePath,
			),
		)
	}
	return validateFilterStageForTraceFields(stage, stagePath)
}

// allowedAggregateItemKeys are the only keys permitted inside each aggregates[] item.
var allowedAggregateItemKeys = map[string]struct{}{
	"function": {},
	"as":       {},
}

// validateAggregateStage checks that "aggregates" is a non-empty array of valid
// items (each with "function" and "as"), then delegates groupby checks.
func validateAggregateStage(stage map[string]interface{}, stagePath string) error {
	rawAggs := stage["aggregates"]
	if rawAggs == nil {
		return newLogValidationError(
			LogValidationMissingRequired,
			stagePath+".aggregates",
			fmt.Sprintf(
				"aggregate stage at %s missing required \"aggregates\" key — use \"aggregates\" (plural), not \"aggs\" or \"aggregations\"",
				stagePath,
			),
		)
	}
	aggs, ok := rawAggs.([]interface{})
	if !ok || len(aggs) == 0 {
		return newLogValidationError(
			LogValidationMissingRequired,
			stagePath+".aggregates",
			fmt.Sprintf(
				"aggregate stage at %s: \"aggregates\" must be a non-empty array — use \"aggregates\" (plural), not \"aggs\" or \"aggregations\"",
				stagePath,
			),
		)
	}

	for i, rawItem := range aggs {
		itemPath := fmt.Sprintf("%s.aggregates[%d]", stagePath, i)
		itemMap, ok := rawItem.(map[string]interface{})
		if !ok {
			continue
		}

		// Reject unknown keys in each item.
		for key := range itemMap {
			if _, ok := allowedAggregateItemKeys[key]; !ok {
				msg := fmt.Sprintf(
					"unknown key %q in aggregates item at %s — allowed keys are \"function\" and \"as\"",
					key, itemPath,
				)
				if key == "alias" {
					msg += `; use "as", not "alias"`
				}
				return newLogValidationError(LogValidationUnknownStageKey, itemPath+"."+key, msg)
			}
		}

		// Require "function" as an object.
		fn := itemMap["function"]
		if fn == nil {
			return newLogValidationError(
				LogValidationMissingRequired,
				itemPath+".function",
				fmt.Sprintf(
					"aggregates item at %s missing required \"function\" key — example: {\"function\":{\"$count\":[]},\"as\":\"count\"}",
					itemPath,
				),
			)
		}
		fnMap, ok := fn.(map[string]interface{})
		if !ok {
			return newLogValidationError(
				LogValidationInvalidField,
				itemPath+".function",
				fmt.Sprintf(
					"aggregates item at %s: \"function\" must be an object like {\"$count\":[]}, not a %T",
					itemPath, fn,
				),
			)
		}
		if err := validateQuantileFunction(fnMap, itemPath+".function"); err != nil {
			return err
		}

		// Require "as" as a non-empty string.
		as, _ := itemMap["as"].(string)
		if strings.TrimSpace(as) == "" {
			return newLogValidationError(
				LogValidationMissingRequired,
				itemPath+".as",
				fmt.Sprintf(
					"aggregates item at %s missing required \"as\" key (output field name) — use \"as\", not \"alias\"",
					itemPath,
				),
			)
		}
	}

	return validateGroupByForTraceFields(stage, stagePath)
}

func validateQuantileFunction(function map[string]interface{}, path string) error {
	rawArgs, ok := function["$quantile"]
	if !ok {
		return nil
	}

	args, ok := rawArgs.([]interface{})
	if !ok || len(args) != 2 {
		return invalidQuantileArgs(path, path+".$quantile")
	}
	level, ok := args[0].(float64)
	if !ok || math.IsNaN(level) || level < 0 || level > 1 {
		return invalidQuantileArgs(path, path+".$quantile[0]")
	}
	if _, ok := args[1].(string); !ok {
		return invalidQuantileArgs(path, path+".$quantile[1]")
	}

	return nil
}

func invalidQuantileArgs(functionPath, errorPath string) error {
	return newLogValidationError(
		LogValidationInvalidField,
		errorPath,
		fmt.Sprintf("$quantile at %s.$quantile must be exactly [level, field], with a numeric level in [0,1] first and a string field second", functionPath),
	)
}

func stageKeyList(allowed map[string]struct{}) string {
	keys := make([]string, 0, len(allowed))
	for k := range allowed {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ", ")
}
