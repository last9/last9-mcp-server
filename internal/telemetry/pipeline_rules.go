package telemetry

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
)

// PipelineArgError is a domain-independent pipeline rule failure carrying the
// caller-relative JSON path. Callers wrap it in their own typed error.
type PipelineArgError struct{ Path, Message string }

func (e *PipelineArgError) Error() string { return e.Message }

type aggregateSpec struct {
	minArgs, maxArgs, fieldArg int
	example                    string
}

func oneArg(name string) aggregateSpec {
	return aggregateSpec{0, 1, 0, fmt.Sprintf(`{"%s":["attributes['latency_ms']"]}`, name)}
}

// aggregateFunctions mirrors the function set shared by the logs and traces
// query engines.
var aggregateFunctions = map[string]aggregateSpec{
	"$count":          {0, 1, 0, `{"$count":[]}`},
	"$rate":           {0, 1, 0, `{"$rate":[]}`},
	"$sum":            {0, 1, 0, `{"$sum":["attributes['bytes']"]}`},
	"$min":            oneArg("$min"),
	"$max":            oneArg("$max"),
	"$avg":            oneArg("$avg"),
	"$median":         oneArg("$median"),
	"$stddev":         oneArg("$stddev"),
	"$stddev_pop":     oneArg("$stddev_pop"),
	"$variance":       oneArg("$variance"),
	"$variance_pop":   oneArg("$variance_pop"),
	"$uniq":           {1, 1, 0, `{"$uniq":["TraceId"]}`},
	"$quantile":       {2, 2, 1, `{"$quantile":[0.99,"attributes['latency_ms']"]}`},
	"$quantile_exact": {2, 2, 1, `{"$quantile_exact":[0.99,"attributes['latency_ms']"]}`},
	"$apdex_score":    {2, 2, 1, `{"$apdex_score":[0.5,"attributes['latency_ms']"]}`},
}

var aggregateFunctionNames = func() string {
	names := make([]string, 0, len(aggregateFunctions))
	for k := range aggregateFunctions {
		names = append(names, k)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}()

var (
	aggregateAliasPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	percentileName        = regexp.MustCompile(`^\$p[0-9]+$`)
	numericLiteral        = regexp.MustCompile(`^[-+]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:[eE][-+]?[0-9]+)?$`)
	comparisonOperators   = map[string]struct{}{"$gt": {}, "$gte": {}, "$lt": {}, "$lte": {}}
)

func isQuantile(name string) bool { return name == "$quantile" || name == "$quantile_exact" }

func unknownFunctionHint(name string) string {
	lower := strings.ToLower(name)
	switch {
	case strings.HasPrefix(lower, "$percentile") || percentileName.MatchString(name):
		return `use {"$quantile":[0.99,"attributes['latency_ms']"]}`
	case strings.Contains(lower, "distinct") || lower == "$unique" || lower == "$cardinality":
		return `use {"$uniq":["TraceId"]}`
	case lower == "$mean" || lower == "$average":
		return `use {"$avg":["attributes['latency_ms']"]}`
	}
	return "valid functions: " + aggregateFunctionNames
}

// ValidateAggregateFunction checks an aggregate/window_aggregate function
// object against the engine's allowlist and argument shapes.
func ValidateAggregateFunction(function map[string]interface{}, functionPath string) *PipelineArgError {
	if len(function) != 1 {
		return &PipelineArgError{functionPath, fmt.Sprintf(`aggregate function at %s must have exactly one key, e.g. {"$count":[]}; got %d`, functionPath, len(function))}
	}
	var name string
	var rawArgs interface{}
	for k, v := range function {
		name, rawArgs = k, v
	}
	spec, ok := aggregateFunctions[strings.TrimSuffix(name, "_over_time")]
	if !ok {
		return &PipelineArgError{functionPath + "." + name, fmt.Sprintf("unknown aggregate function %q at %s; %s", name, functionPath, unknownFunctionHint(name))}
	}
	base := functionPath + "." + name
	twoArg := spec.maxArgs == 2
	shapeErr := func(path string) *PipelineArgError {
		if twoArg {
			want := "[level, field], with a numeric level in [0,1] first and a string field second"
			if !isQuantile(strings.TrimSuffix(name, "_over_time")) {
				want = "[threshold, field], with a numeric threshold first and a string field second"
			}
			return &PipelineArgError{path, fmt.Sprintf("%[1]s at %[2]s.%[1]s must be exactly %[3]s, e.g. %[4]s", name, functionPath, want, spec.example)}
		}
		return &PipelineArgError{path, fmt.Sprintf("%[1]s at %[2]s.%[1]s has wrong arguments; use %[3]s", name, functionPath, spec.example)}
	}

	args, ok := rawArgs.([]interface{})
	if !ok || len(args) < spec.minArgs || len(args) > spec.maxArgs {
		return shapeErr(base)
	}
	if twoArg {
		level, ok := args[0].(float64)
		if !ok || math.IsNaN(level) || (isQuantile(strings.TrimSuffix(name, "_over_time")) && (level < 0 || level > 1)) {
			return shapeErr(base + "[0]")
		}
		if _, ok := args[1].(string); !ok {
			return shapeErr(base + "[1]")
		}
		return nil
	}
	if len(args) == 1 {
		if _, ok := args[0].(string); !ok {
			return shapeErr(base + "[0]")
		}
	}
	return nil
}

// AggregateFieldArg returns the field argument of a known aggregate function.
// ok is false when there is no field to check ($count with "*", wrong shapes).
func AggregateFieldArg(name string, args []interface{}) (field string, index int, ok bool) {
	base := strings.TrimSuffix(name, "_over_time")
	spec, known := aggregateFunctions[base]
	if !known || spec.fieldArg >= len(args) {
		return "", 0, false
	}
	field, isString := args[spec.fieldArg].(string)
	if !isString || (base == "$count" && field == "*") {
		return "", 0, false
	}
	return field, spec.fieldArg, true
}

// ValidateAggregateAlias checks an "as" output name. Empty/missing is left to
// callers, which own whether "as" is required.
func ValidateAggregateAlias(as interface{}, path string) *PipelineArgError {
	if as == nil || as == "" {
		return nil
	}
	if s, ok := as.(string); ok && aggregateAliasPattern.MatchString(s) {
		return nil
	}
	return &PipelineArgError{path, fmt.Sprintf(`"as" at %s must be a plain name (letters, digits, underscore; not starting with a digit), e.g. "p99_latency"; got %q`, path, fmt.Sprint(as))}
}

// CheckNumericComparison requires $gt/$gte/$lt/$lte values to be numbers.
func CheckNumericComparison(operator, field string, value interface{}, path string) *PipelineArgError {
	if _, ok := comparisonOperators[operator]; !ok {
		return nil
	}
	switch v := value.(type) {
	case int, int32, int64, float32, float64:
		return nil
	case string:
		if numericLiteral.MatchString(strings.TrimSpace(v)) {
			return nil
		}
	}
	msg := fmt.Sprintf(`%[1]s at %[2]s compares numbers, so the value must be a number, e.g. {"%[1]s":["attributes['http.status_code']","500"]}; got %[3]q`, operator, path, fmt.Sprint(value))
	if field == "Timestamp" {
		msg += "; to limit time, use start_time_iso/end_time_iso instead of a Timestamp filter"
	}
	return &PipelineArgError{path, msg}
}
