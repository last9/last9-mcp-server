package traces

import "testing"

const gateField = "attributes['http.response_time']"

func numericRegexFilter(field, pattern string) map[string]interface{} {
	return map[string]interface{}{
		"type": "filter",
		"query": map[string]interface{}{
			"$and": []interface{}{
				map[string]interface{}{"$regex": []interface{}{field, pattern}},
			},
		},
	}
}

func canonicalGate(field string) map[string]interface{} {
	return numericRegexFilter(field, `^[0-9]+(?:\.[0-9]+)?$`)
}

// The engine reads a non-numeric string as 0 inside percentiles, so a
// string-backed field needs a numeric $regex first, as logs already require.
func TestSanitizeTraceJSONQuery_PercentileOnStringFieldNeedsNumericGate(t *testing.T) {
	cases := []struct {
		name   string
		stages []map[string]interface{}
		path   string
	}{
		{
			name:   "quantile without gate",
			stages: []map[string]interface{}{aggStage(fn1("$quantile", 0.99, gateField), "p99")},
			path:   "tracejson_query[0].aggregates[0].function.$quantile[1]",
		},
		{
			name:   "quantile_exact on resources without gate",
			stages: []map[string]interface{}{aggStage(fn1("$quantile_exact", 0.95, "resources['queue.depth']"), "p95")},
			path:   "tracejson_query[0].aggregates[0].function.$quantile_exact[1]",
		},
		{
			name:   "window median without gate",
			stages: []map[string]interface{}{winStage(fn1("$median", gateField), "p50")},
			path:   "tracejson_query[0].function.$median[0]",
		},
		{
			name: "gate under $or does not count",
			stages: []map[string]interface{}{
				{"type": "filter", "query": map[string]interface{}{"$or": []interface{}{
					map[string]interface{}{"$regex": []interface{}{gateField, `^[0-9]+$`}},
					map[string]interface{}{"$eq": []interface{}{"ServiceName", "api"}},
				}}},
				aggStage(fn1("$quantile", 0.99, gateField), "p99"),
			},
			path: "tracejson_query[1].aggregates[0].function.$quantile[1]",
		},
		{
			name:   "non-canonical regex",
			stages: []map[string]interface{}{numericRegexFilter(gateField, `\d+`), aggStage(fn1("$quantile", 0.99, gateField), "p99")},
			path:   "tracejson_query[1].aggregates[0].function.$quantile[1]",
		},
		{
			name: "parse after the gate re-produces the field",
			stages: []map[string]interface{}{
				canonicalGate(gateField),
				{"type": "parse", "parser": "json", "labels": map[string]interface{}{"http.response_time": "http.response_time"}},
				aggStage(fn1("$quantile", 0.99, gateField), "p99"),
			},
			path: "tracejson_query[2].aggregates[0].function.$quantile[1]",
		},
		{
			name: "transform after the gate re-produces the field",
			stages: []map[string]interface{}{
				canonicalGate("attributes['endpoint']"),
				{"type": "transform", "transforms": []interface{}{
					map[string]interface{}{"function": fn1("$split", "SpanName", "/", 1.0), "as": []interface{}{"endpoint"}},
				}},
				aggStage(fn1("$quantile", 0.99, "attributes['endpoint']"), "p99"),
			},
			path: "tracejson_query[2].aggregates[0].function.$quantile[1]",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := SanitizeTraceJSONQuery(tc.stages)
			requireTraceInvalidField(t, err, tc.path, "requires a preceding numeric $regex")
		})
	}
}

func TestSanitizeTraceJSONQuery_PercentileAcceptsNumericFieldsAndGatedStrings(t *testing.T) {
	cases := map[string][]map[string]interface{}{
		"Duration quantile":       {aggStage(fn1("$quantile", 0.99, "Duration"), "p99")},
		"Duration median window":  {winStage(fn1("$median", "Duration"), "p50")},
		"gated quantile":          {canonicalGate(gateField), aggStage(fn1("$quantile", 0.99, gateField), "p99")},
		"gated quantile_exact":    {canonicalGate(gateField), aggStage(fn1("$quantile_exact", 0.5, gateField), "p50")},
		"gated median":            {canonicalGate(gateField), winStage(fn1("$median", gateField), "p50")},
		"avg is not gated":        {aggStage(fn1("$avg", gateField), "avg")},
		"gate before other parse": {canonicalGate(gateField), {"type": "parse", "parser": "json", "labels": map[string]interface{}{"other": "other"}}, aggStage(fn1("$quantile", 0.99, gateField), "p99")},
	}
	for name, stages := range cases {
		t.Run(name, func(t *testing.T) {
			if err := SanitizeTraceJSONQuery(stages); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
