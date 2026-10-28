package traces

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

func fnCase(name, suffix, substr string, fn map[string]interface{}) aggregateRuleCase {
	return aggregateRuleCase{name: name, fn: fn, suffix: suffix, substr: substr}
}

func fn1(op string, args ...interface{}) map[string]interface{} {
	return map[string]interface{}{op: append([]interface{}{}, args...)}
}

func aggStage(fn map[string]interface{}, as interface{}) map[string]interface{} {
	entry := map[string]interface{}{"function": fn}
	if as != nil {
		entry["as"] = as
	}
	return map[string]interface{}{"type": "aggregate", "aggregates": []interface{}{entry}}
}

func winStage(fn map[string]interface{}, as string) map[string]interface{} {
	return map[string]interface{}{
		"type": "window_aggregate", "function": fn, "as": as,
		"window": []interface{}{"5", "minutes"},
	}
}

func requireTraceInvalidField(t *testing.T, err error, path, substr string) {
	t.Helper()
	var perr *tracePipelineError
	if !errors.As(err, &perr) {
		t.Fatalf("expected *tracePipelineError, got %T (%v)", err, err)
	}
	if perr.Category() != traceCategoryInvalidField {
		t.Fatalf("category = %q (%v)", perr.Category(), err)
	}
	if perr.Path() != path {
		t.Fatalf("path = %q, want %q (%v)", perr.Path(), path, err)
	}
	if !strings.Contains(err.Error(), substr) {
		t.Fatalf("error %q does not contain %q", err.Error(), substr)
	}
}

const trF = "Duration"

func TestSanitizeTraceJSONQuery_AcceptsEveryAggregateFunction(t *testing.T) {
	fns := []map[string]interface{}{
		fn1("$count"), fn1("$count", "*"), fn1("$rate"),
		fn1("$sum", trF), fn1("$min", trF), fn1("$max", trF), fn1("$avg", trF),
		fn1("$median", trF), fn1("$stddev", trF), fn1("$stddev_pop", trF),
		fn1("$variance", trF), fn1("$variance_pop", trF),
		fn1("$uniq", "TraceId"), fn1("$uniq", "resources['k8s.pod.name']"),
		fn1("$avg", "attributes['http.response_time']"),
		fn1("$quantile", 0.99, trF), fn1("$quantile_exact", 0.99, trF),
		fn1("$apdex_score", 0.5, trF), fn1("$avg_over_time", trF),
	}
	for _, fn := range fns {
		for _, as := range []string{"v", "p99_latency", "_x1", "39b_1", "1x"} {
			if err := SanitizeTraceJSONQuery([]map[string]interface{}{aggStage(fn, as)}); err != nil {
				t.Errorf("aggregate %v as=%q: %v", fn, as, err)
			}
			if err := SanitizeTraceJSONQuery([]map[string]interface{}{winStage(fn, as)}); err != nil {
				t.Errorf("window %v as=%q: %v", fn, as, err)
			}
		}
	}
	if err := SanitizeTraceJSONQuery([]map[string]interface{}{aggStage(fn1("$count"), nil)}); err != nil {
		t.Errorf("missing as: %v", err)
	}
	stages := []map[string]interface{}{
		{"type": "transform", "transforms": []interface{}{map[string]interface{}{
			"function": map[string]interface{}{"$split": []interface{}{"SpanName", "/", 1}},
			"as":       []interface{}{"endpoint"},
		}}},
		aggStage(fn1("$uniq", "endpoint"), "n"),
	}
	if err := SanitizeTraceJSONQuery(stages); err != nil {
		t.Errorf("transform alias: %v", err)
	}
}

func TestSanitizeTraceJSONQuery_RejectsInvalidAggregateFunctions(t *testing.T) {
	const p = "tracejson_query[0].aggregates[0].function"
	cases := []aggregateRuleCase{
		fnCase("p99", ".$p99", "$quantile", fn1("$p99", trF)),
		fnCase("percentile", ".$percentile", "$quantile", fn1("$percentile", 99.0, trF)),
		fnCase("count_distinct", ".$count_distinct", "$uniq", fn1("$count_distinct", trF)),
		fnCase("foo", ".$foo", "$apdex_score", fn1("$foo")),

		fnCase("two keys", "", "exactly one key", map[string]interface{}{"$count": []interface{}{}, "$avg": []interface{}{trF}}),
		fnCase("empty", "", "exactly one key", map[string]interface{}{}),
		fnCase("uniq none", ".$uniq", `{"$uniq":["TraceId"]}`, fn1("$uniq")),
		fnCase("uniq two", ".$uniq", `{"$uniq":["TraceId"]}`, fn1("$uniq", trF, "TraceId")),
		fnCase("avg two", ".$avg", "wrong arguments", fn1("$avg", trF, trF)),
		fnCase("count string", ".$count", "wrong arguments", map[string]interface{}{"$count": "x"}),
		fnCase("avg number", ".$avg[0]", "wrong arguments", fn1("$avg", 42.0)),
		fnCase("qe one", ".$quantile_exact", "[level, field]", fn1("$quantile_exact", 0.99)),
		fnCase("qe swapped", ".$quantile_exact[0]", "[level, field]", fn1("$quantile_exact", trF, 0.99)),
		fnCase("qe range", ".$quantile_exact[0]", "[level, field]", fn1("$quantile_exact", 1.5, trF)),
		fnCase("apdex one", ".$apdex_score", "[threshold, field]", fn1("$apdex_score", trF)),
		fnCase("apdex string", ".$apdex_score[0]", "[threshold, field]", fn1("$apdex_score", "0.5", trF)),

		fnCase("avg ref", ".$avg[0]", "invalid aggregate field", fn1("$avg", "Duration) OR (1=1")),
		fnCase("numeric ref", ".$sum[0]", "invalid aggregate field", fn1("$sum", "123")),
		fnCase("quantile ref", ".$quantile[1]", "invalid aggregate field", fn1("$quantile", 0.95, "Duration) OR (1=1")),
		fnCase("uniq ref", ".$uniq[0]", "invalid aggregate field", fn1("$uniq", "attributes['a') OR ('1'='1']")),
		fnCase("uniq backslash", ".$uniq[0]", "invalid aggregate field", fn1("$uniq", `attributes['a\\b']`)),
		fnCase("double quotes", ".$sum[0]", "single quotes", fn1("$sum", `attributes["http.status_code"]`)),
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := SanitizeTraceJSONQuery([]map[string]interface{}{aggStage(c.fn, "v")})
			requireTraceInvalidField(t, err, p+c.suffix, c.substr)
		})
	}
}

func TestSanitizeTraceJSONQuery_AcceptsSupportedAggregateFieldSyntax(t *testing.T) {
	for _, field := range []string{`attributes['label[0]']`, `resources['deployment[zone]']`, "Events.Name[1]"} {
		for _, stage := range []map[string]interface{}{
			aggStage(fn1("$uniq", field), "n"),
			winStage(fn1("$uniq", field), "n"),
		} {
			if err := SanitizeTraceJSONQuery([]map[string]interface{}{stage}); err != nil {
				t.Errorf("%s field %q: %v", stage["type"], field, err)
			}
		}
	}
}

func TestSanitizeTraceJSONQueryDefaultsQuantileExactToPreviousResult(t *testing.T) {
	for _, operator := range []string{"$quantile_exact", "$quantile_exact_over_time"} {
		t.Run(operator, func(t *testing.T) {
			for kind, consumer := range map[string]func(map[string]interface{}, string) map[string]interface{}{
				"aggregate": func(fn map[string]interface{}, as string) map[string]interface{} { return aggStage(fn, as) },
				"window":    winStage,
			} {
				stages := []map[string]interface{}{
					winStage(fn1("$count"), "__result__"),
					consumer(fn1(operator, 0.99), "p99"),
				}
				if err := SanitizeTraceJSONQuery(stages); err != nil {
					t.Fatalf("%s consumer: %v", kind, err)
				}
				var args []interface{}
				if kind == "aggregate" {
					entry := stages[1]["aggregates"].([]interface{})[0].(map[string]interface{})
					args = entry["function"].(map[string]interface{})[operator].([]interface{})
				} else {
					args = stages[1]["function"].(map[string]interface{})[operator].([]interface{})
				}
				if len(args) != 2 || args[1] != "__result__" {
					t.Errorf("%s args = %#v, want an implicit __result__ field", kind, args)
				}

				invalid := []map[string]interface{}{winStage(fn1("$count"), "__result__"), consumer(fn1(operator, 1.5), "p99")}
				if err := SanitizeTraceJSONQuery(invalid); err == nil || !strings.Contains(err.Error(), "numeric level in [0,1]") {
					t.Errorf("%s out-of-range level error = %v", kind, err)
				}
			}
		})
	}
}

func TestSanitizeTraceJSONQuery_AggregatesEarlierDigitLeadingAlias(t *testing.T) {
	for _, as := range []string{"39b_1", "1x", "9_"} {
		for _, first := range []map[string]interface{}{
			aggStage(fn1("$count"), as),
			winStage(fn1("$count"), as),
		} {
			stages := []map[string]interface{}{first, aggStage(fn1("$sum", as), "total")}
			if err := SanitizeTraceJSONQuery(stages); err != nil {
				t.Errorf("%s alias %q: %v", first["type"], as, err)
			}
		}
	}
}

func TestSanitizeTraceJSONQuery_RejectsInvalidAggregateAlias(t *testing.T) {
	for _, as := range []string{"p99 latency", "123", "a;b", "p99-latency"} {
		err := SanitizeTraceJSONQuery([]map[string]interface{}{aggStage(fn1("$count"), as)})
		requireTraceInvalidField(t, err, "tracejson_query[0].aggregates[0].as", "p99_latency")
		err = SanitizeTraceJSONQuery([]map[string]interface{}{winStage(fn1("$count"), as)})
		requireTraceInvalidField(t, err, "tracejson_query[0].as", "p99_latency")
	}
}

func TestSanitizeTraceJSONQuery_RequiresNumericComparisonValues(t *testing.T) {
	bad := []struct {
		op     string
		args   []interface{}
		substr string
	}{
		{"$gt", []interface{}{"Duration", "1s"}, ""},
		{"$lt", []interface{}{"Duration", "1 OR 1=1"}, ""},
		{"$lte", []interface{}{"Duration", true}, ""},
		{"$gt", []interface{}{"Duration", nil}, ""},
		{"$gte", []interface{}{"Timestamp", "2024-03-01T00:00:00Z"}, "start_time_iso"},
	}
	for _, stageType := range []string{"filter", "where"} {
		for _, c := range bad {
			stage := map[string]interface{}{"type": stageType, "query": map[string]interface{}{c.op: c.args}}
			err := SanitizeTraceJSONQuery([]map[string]interface{}{stage})
			requireTraceInvalidField(t, err, "tracejson_query[0].query."+c.op+"[1]", "must be a number")
			if c.substr != "" && !strings.Contains(err.Error(), c.substr) {
				t.Errorf("%v missing %q", err, c.substr)
			}
		}
	}
	_, err := SanitizeTraceFilterConditions([]map[string]interface{}{{"$gt": []interface{}{"Duration", "abc"}}}, "filters")
	requireTraceInvalidField(t, err, "filters[0].$gt[1]", "must be a number")

	for _, v := range []interface{}{"1000000000", 1e9, "1.5e3", "-1", int64(5)} {
		stage := map[string]interface{}{"type": "filter", "query": map[string]interface{}{"$gt": []interface{}{"Duration", v}}}
		if err := SanitizeTraceJSONQuery([]map[string]interface{}{stage}); err != nil {
			t.Errorf("value %v: %v", v, err)
		}
	}
}
