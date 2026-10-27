package logs

import (
	"errors"
	"strings"
	"testing"
)

type aggregateRuleCase struct {
	name   string
	fn     map[string]interface{}
	suffix string
	substr string
}

func fnCase(name string, suffix, substr string, fn map[string]interface{}) aggregateRuleCase {
	return aggregateRuleCase{name: name, fn: fn, suffix: suffix, substr: substr}
}

func fn1(op string, args ...interface{}) map[string]interface{} {
	return map[string]interface{}{op: append([]interface{}{}, args...)}
}

func aggregateStage(fn map[string]interface{}, as string) map[string]interface{} {
	return map[string]interface{}{
		"type": "aggregate",
		"aggregates": []interface{}{
			map[string]interface{}{"function": fn, "as": as},
		},
	}
}

func windowStage(fn map[string]interface{}, as string) map[string]interface{} {
	return map[string]interface{}{
		"type":     "window_aggregate",
		"function": fn,
		"as":       as,
		"window":   []interface{}{"5", "minutes"},
	}
}

func requireInvalidField(t *testing.T, err error, path, substr string) {
	t.Helper()
	var verr *LogPipelineValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("expected *LogPipelineValidationError, got %T (%v)", err, err)
	}
	if verr.Category != LogValidationInvalidField {
		t.Fatalf("category = %q, want %q (%v)", verr.Category, LogValidationInvalidField, err)
	}
	if verr.Path != path {
		t.Fatalf("path = %q, want %q (%v)", verr.Path, path, err)
	}
	if !strings.Contains(err.Error(), substr) {
		t.Fatalf("error %q does not contain %q", err.Error(), substr)
	}
}

const aggF = "attributes['latency_ms']"

func TestPrepareLogJSONQueryAcceptsEveryAggregateFunction(t *testing.T) {
	fns := []map[string]interface{}{
		fn1("$count"), fn1("$count", "*"), fn1("$rate"),
		fn1("$sum", aggF), fn1("$min", aggF), fn1("$max", aggF), fn1("$avg", aggF),
		fn1("$median", aggF), fn1("$stddev", aggF), fn1("$stddev_pop", aggF),
		fn1("$variance", aggF), fn1("$variance_pop", aggF),
		fn1("$uniq", "TraceId"),
		fn1("$quantile", 0.99, aggF), fn1("$quantile_exact", 0.99, aggF),
		fn1("$apdex_score", 0.5, aggF), fn1("$avg_over_time", aggF),
	}
	for _, fn := range fns {
		for _, as := range []string{"v", "p99_latency", "_x1", "39b_1", "1x"} {
			for kind, mk := range map[string]func(map[string]interface{}, string) map[string]interface{}{"aggregate": aggregateStage, "window": windowStage} {
				stages := []map[string]interface{}{testCanonicalNumericFilter(aggF), mk(fn, as)}
				if _, err := prepareLogJSONQuery(stages, "logjson_query"); err != nil {
					t.Errorf("%s %v as=%q: unexpected error: %v", kind, fn, as, err)
				}
			}
		}
	}
}

func TestPrepareLogJSONQueryRejectsInvalidAggregateFunctions(t *testing.T) {
	const p = "logjson_query[0].aggregates[0].function"
	cases := []aggregateRuleCase{
		fnCase("p99", ".$p99", "$quantile", fn1("$p99", aggF)),
		fnCase("percentile", ".$percentile", "$quantile", fn1("$percentile", 99.0, aggF)),
		fnCase("count_distinct", ".$count_distinct", "$uniq", fn1("$count_distinct", aggF)),
		fnCase("foo", ".$foo", "$apdex_score", fn1("$foo")),

		fnCase("two keys", "", "exactly one key", map[string]interface{}{"$count": []interface{}{}, "$avg": []interface{}{aggF}}),
		fnCase("empty", "", "exactly one key", map[string]interface{}{}),
		fnCase("uniq none", ".$uniq", `{"$uniq":["TraceId"]}`, fn1("$uniq")),
		fnCase("uniq two", ".$uniq", `{"$uniq":["TraceId"]}`, fn1("$uniq", aggF, "TraceId")),
		fnCase("avg two", ".$avg", "wrong arguments", fn1("$avg", aggF, aggF)),
		fnCase("count string", ".$count", "wrong arguments", map[string]interface{}{"$count": "x"}),
		fnCase("avg number", ".$avg[0]", "wrong arguments", fn1("$avg", 42.0)),
		fnCase("qe one", ".$quantile_exact", "[level, field]", fn1("$quantile_exact", 0.99)),
		fnCase("qe swapped", ".$quantile_exact[0]", "[level, field]", fn1("$quantile_exact", aggF, 0.99)),
		fnCase("qe range", ".$quantile_exact[0]", "[level, field]", fn1("$quantile_exact", 1.5, aggF)),
		fnCase("apdex one", ".$apdex_score", "[threshold, field]", fn1("$apdex_score", aggF)),
		fnCase("apdex string", ".$apdex_score[0]", "[threshold, field]", fn1("$apdex_score", "0.5", aggF)),

		fnCase("uniq ref", ".$uniq[0]", "invalid log field reference", fn1("$uniq", "Duration) OR (1=1")),
		fnCase("count ref", ".$count[0]", "invalid log field reference", fn1("$count", "Body) OR (1=1")),
		fnCase("median ref", ".$median[0]", "invalid log field reference", fn1("$median", "attributes['a') OR ('1'='1']")),
		fnCase("qe ref", ".$quantile_exact[1]", "invalid log field reference", fn1("$quantile_exact", 0.99, "Duration) OR (1=1")),
		fnCase("apdex ref", ".$apdex_score[1]", "invalid log field reference", fn1("$apdex_score", 0.5, "x) OR (1=1")),
		fnCase("backslash", ".$avg[0]", "invalid log field reference", fn1("$avg", `attributes['a\']`)),
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := prepareLogJSONQuery([]map[string]interface{}{aggregateStage(c.fn, "v")}, "logjson_query")
			requireInvalidField(t, err, p+c.suffix, c.substr)
		})
	}
}

func TestPrepareLogJSONQueryRejectsInvalidAggregateAlias(t *testing.T) {
	for _, as := range []string{"p99 latency", "123", "a;b", "p99-latency"} {
		_, err := prepareLogJSONQuery([]map[string]interface{}{aggregateStage(fn1("$count"), as)}, "logjson_query")
		requireInvalidField(t, err, "logjson_query[0].aggregates[0].as", "p99_latency")
		_, err = prepareLogJSONQuery([]map[string]interface{}{windowStage(fn1("$count"), as)}, "logjson_query")
		requireInvalidField(t, err, "logjson_query[0].as", "p99_latency")
	}
	_, err := prepareLogJSONQuery([]map[string]interface{}{windowStage(fn1("$p99", aggF), "v")}, "logjson_query")
	requireInvalidField(t, err, "logjson_query[0].function.$p99", "$quantile")
	two := map[string]interface{}{"$count": []interface{}{}, "$avg": []interface{}{aggF}}
	_, err = prepareLogJSONQuery([]map[string]interface{}{windowStage(two, "v")}, "logjson_query")
	requireInvalidField(t, err, "logjson_query[0].function", "exactly one key")
}

func TestPrepareLogJSONQueryQuantileExactNeedsNumericGuard(t *testing.T) {
	fn := fn1("$quantile_exact", 0.99, aggF)
	_, err := prepareLogJSONQuery([]map[string]interface{}{aggregateStage(fn, "v")}, "logjson_query")
	requireInvalidField(t, err, "logjson_query[0].aggregates[0].function.$quantile_exact[1]", "canonical anchored numeric $regex")
	_, err = prepareLogJSONQuery([]map[string]interface{}{testCanonicalNumericFilter(aggF), aggregateStage(fn, "v")}, "logjson_query")
	if err != nil {
		t.Fatalf("unexpected error with guard: %v", err)
	}
}

func TestPrepareLogJSONQueryMedianNeedsNumericGuard(t *testing.T) {
	fn := fn1("$median", aggF)
	_, err := prepareLogJSONQuery([]map[string]interface{}{aggregateStage(fn, "v")}, "logjson_query")
	requireInvalidField(t, err, "logjson_query[0].aggregates[0].function.$median[0]", "canonical anchored numeric $regex")
	_, err = prepareLogJSONQuery([]map[string]interface{}{testCanonicalNumericFilter(aggF), aggregateStage(fn, "v")}, "logjson_query")
	if err != nil {
		t.Fatalf("unexpected error with guard: %v", err)
	}
}

func cmpFilter(op string, args ...interface{}) map[string]interface{} {
	return map[string]interface{}{"type": "filter", "query": map[string]interface{}{op: args}}
}

func TestPrepareLogJSONQueryRequiresNumericComparisonValues(t *testing.T) {
	const f = "attributes['http.status_code']"
	bad := []struct {
		op     string
		args   []interface{}
		substr string
	}{
		{"$gt", []interface{}{f, "abc"}, ""},
		{"$lt", []interface{}{f, "5 OR 1=1"}, ""},
		{"$lte", []interface{}{f, true}, ""},
		{"$gt", []interface{}{f, ""}, ""},
		{"$gte", []interface{}{"Timestamp", "2026-06-04T00:00:00Z"}, "start_time_iso"},
	}
	for _, c := range bad {
		_, err := prepareLogJSONQuery([]map[string]interface{}{cmpFilter(c.op, c.args...)}, "logjson_query")
		requireInvalidField(t, err, "logjson_query[0].query."+c.op+"[1]", "must be a number")
		if c.substr != "" && !strings.Contains(err.Error(), c.substr) {
			t.Errorf("%v missing %q", err, c.substr)
		}
	}
	nested := map[string]interface{}{"type": "filter", "query": map[string]interface{}{
		"$and": []interface{}{map[string]interface{}{"$gt": []interface{}{f, "abc"}}},
	}}
	_, err := prepareLogJSONQuery([]map[string]interface{}{nested}, "logjson_query")
	requireInvalidField(t, err, "logjson_query[0].query.$and[0].$gt[1]", "must be a number")

	for _, v := range []interface{}{"500", 500.0, "1.5", "-2", "1e3"} {
		if _, err := prepareLogJSONQuery([]map[string]interface{}{cmpFilter("$gt", f, v)}, "logjson_query"); err != nil {
			t.Errorf("value %v: unexpected error %v", v, err)
		}
	}
}
