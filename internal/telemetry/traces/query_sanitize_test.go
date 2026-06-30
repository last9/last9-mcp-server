package traces

import (
	"reflect"
	"strings"
	"testing"
)

func TestSanitizeTraceJSONQuery_ValidPipelines(t *testing.T) {
	tests := []struct {
		name   string
		stages []map[string]interface{}
	}{
		{
			name: "simple filter",
			stages: []map[string]interface{}{
				{"type": "filter", "query": map[string]interface{}{
					"$eq": []interface{}{"ServiceName", "api"},
				}},
			},
		},
		{
			name: "filter with $and",
			stages: []map[string]interface{}{
				{"type": "filter", "query": map[string]interface{}{
					"$and": []interface{}{
						map[string]interface{}{"$eq": []interface{}{"ServiceName", "api"}},
						map[string]interface{}{"$eq": []interface{}{"StatusCode", "STATUS_CODE_ERROR"}},
					},
				}},
			},
		},
		{
			name: "filter then aggregate with count",
			stages: []map[string]interface{}{
				{"type": "filter", "query": map[string]interface{}{"$eq": []interface{}{"StatusCode", "STATUS_CODE_ERROR"}}},
				{
					"type": "aggregate",
					"aggregates": []interface{}{
						map[string]interface{}{"function": map[string]interface{}{"$count": []interface{}{}}, "as": "count"},
					},
					"groupby": map[string]interface{}{"ServiceName": "service"},
				},
			},
		},
		{
			name: "aggregate with multiple functions",
			stages: []map[string]interface{}{
				{"type": "filter", "query": map[string]interface{}{"$neq": []interface{}{"Duration", ""}}},
				{
					"type": "aggregate",
					"aggregates": []interface{}{
						map[string]interface{}{"function": map[string]interface{}{"$avg": []interface{}{"Duration"}}, "as": "avg_duration"},
						map[string]interface{}{"function": map[string]interface{}{"$max": []interface{}{"Duration"}}, "as": "max_duration"},
						map[string]interface{}{"function": map[string]interface{}{"$quantile": []interface{}{0.95, "Duration"}}, "as": "p95"},
					},
					"groupby": map[string]interface{}{"ServiceName": "service"},
				},
			},
		},
		{
			name: "aggregate without groupby",
			stages: []map[string]interface{}{
				{"type": "filter", "query": map[string]interface{}{"$eq": []interface{}{"StatusCode", "STATUS_CODE_ERROR"}}},
				{
					"type": "aggregate",
					"aggregates": []interface{}{
						map[string]interface{}{"function": map[string]interface{}{"$count": []interface{}{}}, "as": "total"},
					},
				},
			},
		},
		{
			name: "window_aggregate",
			stages: []map[string]interface{}{
				{"type": "filter", "query": map[string]interface{}{"$neq": []interface{}{"TraceId", ""}}},
				{
					"type":     "window_aggregate",
					"function": map[string]interface{}{"$count": []interface{}{}},
					"as":       "requests_per_min",
					"window":   []interface{}{"5", "minutes"},
				},
			},
		},
		{
			name: "filter after aggregate (HAVING-style)",
			stages: []map[string]interface{}{
				{"type": "filter", "query": map[string]interface{}{"$eq": []interface{}{"StatusCode", "STATUS_CODE_ERROR"}}},
				{
					"type": "aggregate",
					"aggregates": []interface{}{
						map[string]interface{}{"function": map[string]interface{}{"$count": []interface{}{}}, "as": "error_count"},
					},
					"groupby": map[string]interface{}{"ServiceName": "service"},
				},
				{"type": "filter", "query": map[string]interface{}{"$gt": []interface{}{"error_count", "10"}}},
			},
		},
		{
			name: "parse stage",
			stages: []map[string]interface{}{
				{"type": "filter", "query": map[string]interface{}{"$neq": []interface{}{"TraceId", ""}}},
				{"type": "parse", "parser": "json", "labels": map[string]interface{}{"user_id": "uid"}},
			},
		},
		{
			name: "transform stage",
			stages: []map[string]interface{}{
				{"type": "filter", "query": map[string]interface{}{"$neq": []interface{}{"TraceId", ""}}},
				{
					"type": "transform",
					"transforms": []interface{}{
						map[string]interface{}{
							"function": map[string]interface{}{"$split": []interface{}{"SpanName", "/", 1}},
							"as":       "endpoint",
						},
					},
				},
			},
		},
		{
			name: "where alias for filter",
			stages: []map[string]interface{}{
				{"type": "where", "query": map[string]interface{}{"$eq": []interface{}{"ServiceName", "api"}}},
			},
		},
		{
			name: "all statistical aggregate functions",
			stages: []map[string]interface{}{
				{"type": "filter", "query": map[string]interface{}{"$neq": []interface{}{"Duration", ""}}},
				{
					"type": "aggregate",
					"aggregates": []interface{}{
						map[string]interface{}{"function": map[string]interface{}{"$median": []interface{}{"Duration"}}, "as": "median"},
						map[string]interface{}{"function": map[string]interface{}{"$stddev": []interface{}{"Duration"}}, "as": "stddev"},
						map[string]interface{}{"function": map[string]interface{}{"$variance": []interface{}{"Duration"}}, "as": "variance"},
						map[string]interface{}{"function": map[string]interface{}{"$quantile_exact": []interface{}{0.99, "Duration"}}, "as": "p99_exact"},
						map[string]interface{}{"function": map[string]interface{}{"$apdex_score": []interface{}{0.5, "Duration"}}, "as": "apdex"},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := sanitizeTraceJSONQuery(tt.stages); err != nil {
				t.Errorf("expected no error, got: %v", err)
			}
		})
	}
}

func TestSanitizeTraceJSONQuery_WrongAggregateKeys(t *testing.T) {
	tests := []struct {
		name        string
		stages      []map[string]interface{}
		errContains string
	}{
		{
			name: "aggregations instead of aggregates",
			stages: []map[string]interface{}{
				{
					"type": "aggregate",
					"aggregations": []interface{}{
						map[string]interface{}{"function": map[string]interface{}{"$count": []interface{}{}}, "as": "count"},
					},
				},
			},
			errContains: `"aggregations"`,
		},
		{
			name: "group_by instead of groupby",
			stages: []map[string]interface{}{
				{
					"type": "aggregate",
					"aggregates": []interface{}{
						map[string]interface{}{"function": map[string]interface{}{"$count": []interface{}{}}, "as": "count"},
					},
					"group_by": []interface{}{"ServiceName"},
				},
			},
			errContains: `"group_by"`,
		},
		{
			name: "alias instead of as in aggregate entry",
			stages: []map[string]interface{}{
				{
					"type": "aggregate",
					"aggregates": []interface{}{
						map[string]interface{}{"function": map[string]interface{}{"$count": []interface{}{}}, "alias": "count"},
					},
				},
			},
			errContains: `"alias"`,
		},
		{
			name: "function as string instead of object",
			stages: []map[string]interface{}{
				{
					"type": "aggregate",
					"aggregates": []interface{}{
						map[string]interface{}{"function": "count", "as": "count"},
					},
				},
			},
			errContains: `"function" must be an object`,
		},
		{
			name: "all three wrong keys together",
			stages: []map[string]interface{}{
				{
					"type":         "aggregate",
					"aggregations": []interface{}{},
					"group_by":     []interface{}{"ServiceName"},
				},
			},
			errContains: `"aggregations"`,
		},
		{
			name: "wrong key in second stage",
			stages: []map[string]interface{}{
				{"type": "filter", "query": map[string]interface{}{"$eq": []interface{}{"StatusCode", "STATUS_CODE_ERROR"}}},
				{
					"type": "aggregate",
					"aggregates": []interface{}{
						map[string]interface{}{"function": map[string]interface{}{"$count": []interface{}{}}, "alias": "n"},
					},
				},
			},
			errContains: `"alias"`,
		},
		{
			name: "aggregations at filter stage (wrong position)",
			stages: []map[string]interface{}{
				{"type": "filter", "aggregations": []interface{}{}, "query": map[string]interface{}{}},
			},
			errContains: `"aggregations"`,
		},
		{
			name: "group_by at filter stage",
			stages: []map[string]interface{}{
				{"type": "filter", "group_by": []interface{}{"ServiceName"}, "query": map[string]interface{}{}},
			},
			errContains: `"group_by"`,
		},
		{
			name: "string function in second aggregate entry",
			stages: []map[string]interface{}{
				{
					"type": "aggregate",
					"aggregates": []interface{}{
						map[string]interface{}{"function": map[string]interface{}{"$count": []interface{}{}}, "as": "count"},
						map[string]interface{}{"function": "avg", "as": "avg_dur"},
					},
				},
			},
			errContains: `"function" must be an object`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := sanitizeTraceJSONQuery(tt.stages)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.errContains)
			}
			if !strings.Contains(err.Error(), tt.errContains) {
				t.Errorf("expected error to contain %q, got: %v", tt.errContains, err)
			}
		})
	}
}

func TestSanitizeTraceJSONQuery_ErrorMessages(t *testing.T) {
	t.Run("aggregations error includes correct key name and example", func(t *testing.T) {
		stages := []map[string]interface{}{
			{"type": "aggregate", "aggregations": []interface{}{}},
		}
		err := sanitizeTraceJSONQuery(stages)
		if err == nil {
			t.Fatal("expected error")
		}
		msg := err.Error()
		if !strings.Contains(msg, "aggregates") {
			t.Errorf("error should mention correct key 'aggregates', got: %v", msg)
		}
		if !strings.Contains(msg, "tracejson_query[0]") {
			t.Errorf("error should include stage index, got: %v", msg)
		}
	})

	t.Run("group_by error includes correct key name and example", func(t *testing.T) {
		stages := []map[string]interface{}{
			{"type": "aggregate", "group_by": []interface{}{}, "aggregates": []interface{}{}},
		}
		err := sanitizeTraceJSONQuery(stages)
		if err == nil {
			t.Fatal("expected error")
		}
		msg := err.Error()
		if !strings.Contains(msg, "groupby") {
			t.Errorf("error should mention correct key 'groupby', got: %v", msg)
		}
	})

	t.Run("string function error includes object example", func(t *testing.T) {
		stages := []map[string]interface{}{
			{
				"type": "aggregate",
				"aggregates": []interface{}{
					map[string]interface{}{"function": "count", "as": "n"},
				},
			},
		}
		err := sanitizeTraceJSONQuery(stages)
		if err == nil {
			t.Fatal("expected error")
		}
		msg := err.Error()
		if !strings.Contains(msg, "$count") {
			t.Errorf("error should show $count example, got: %v", msg)
		}
		if !strings.Contains(msg, "tracejson_query[0].aggregates[0]") {
			t.Errorf("error should include path to offending entry, got: %v", msg)
		}
	})

	t.Run("alias error includes correct key name", func(t *testing.T) {
		stages := []map[string]interface{}{
			{
				"type": "aggregate",
				"aggregates": []interface{}{
					map[string]interface{}{"function": map[string]interface{}{"$count": []interface{}{}}, "alias": "n"},
				},
			},
		}
		err := sanitizeTraceJSONQuery(stages)
		if err == nil {
			t.Fatal("expected error")
		}
		msg := err.Error()
		if !strings.Contains(msg, "as") {
			t.Errorf("error should mention correct key 'as', got: %v", msg)
		}
	})
}

func TestSanitizeTraceJSONQuery_InvalidFilterConditionKeys(t *testing.T) {
	tests := []struct {
		name        string
		stages      []map[string]interface{}
		errContains string
	}{
		{
			name: "bare service key in filter query",
			stages: []map[string]interface{}{
				{"type": "filter", "query": map[string]interface{}{
					"service": "gateway-k8s-hydra",
				}},
			},
			errContains: `"service"`,
		},
		{
			name: "bare key mixed with $and",
			stages: []map[string]interface{}{
				{"type": "filter", "query": map[string]interface{}{
					"$and": []interface{}{
						map[string]interface{}{"$eq": []interface{}{"ServiceName", "api"}},
					},
					"service": "gateway-k8s-hydra",
				}},
			},
			errContains: `"service"`,
		},
		{
			name: "bare key nested inside $and",
			stages: []map[string]interface{}{
				{"type": "filter", "query": map[string]interface{}{
					"$and": []interface{}{
						map[string]interface{}{"service": "gateway-k8s-hydra"},
					},
				}},
			},
			errContains: `"service"`,
		},
		{
			name: "bare ServiceName key at top level",
			stages: []map[string]interface{}{
				{"type": "filter", "query": map[string]interface{}{
					"ServiceName": "checkout",
				}},
			},
			errContains: `"ServiceName"`,
		},
		{
			name: "where alias also validates filter conditions",
			stages: []map[string]interface{}{
				{"type": "where", "query": map[string]interface{}{
					"service": "api",
				}},
			},
			errContains: `"service"`,
		},
		{
			name: "bare key nested inside $or",
			stages: []map[string]interface{}{
				{"type": "filter", "query": map[string]interface{}{
					"$or": []interface{}{
						map[string]interface{}{"status": "error"},
					},
				}},
			},
			errContains: `"status"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := sanitizeTraceJSONQuery(tt.stages)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.errContains)
			}
			if !strings.Contains(err.Error(), tt.errContains) {
				t.Errorf("expected error to contain %q, got: %v", tt.errContains, err)
			}
		})
	}
}

func TestSanitizeTraceJSONQuery_ValidFilterOperators(t *testing.T) {
	tests := []struct {
		name   string
		stages []map[string]interface{}
	}{
		{
			name: "all comparison operators",
			stages: []map[string]interface{}{
				{"type": "filter", "query": map[string]interface{}{
					"$and": []interface{}{
						map[string]interface{}{"$gt": []interface{}{"Duration", "1000"}},
						map[string]interface{}{"$lt": []interface{}{"Duration", "5000"}},
						map[string]interface{}{"$gte": []interface{}{"Duration", "500"}},
						map[string]interface{}{"$lte": []interface{}{"Duration", "9000"}},
						map[string]interface{}{"$neq": []interface{}{"StatusCode", "STATUS_CODE_OK"}},
					},
				}},
			},
		},
		{
			name: "string operators",
			stages: []map[string]interface{}{
				{"type": "filter", "query": map[string]interface{}{
					"$or": []interface{}{
						map[string]interface{}{"$contains": []interface{}{"SpanName", "checkout"}},
						map[string]interface{}{"$icontains": []interface{}{"SpanName", "payment"}},
						map[string]interface{}{"$regex": []interface{}{"SpanName", "^/api/"}},
						map[string]interface{}{"$ieq": []interface{}{"ServiceName", "API"}},
					},
				}},
			},
		},
		{
			name: "$notnull operator",
			stages: []map[string]interface{}{
				{"type": "filter", "query": map[string]interface{}{
					"$notnull": []interface{}{"TraceId"},
				}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := sanitizeTraceJSONQuery(tt.stages); err != nil {
				t.Errorf("expected no error, got: %v", err)
			}
		})
	}
}

func TestSanitizeTraceJSONQuery_InvalidFieldReferences(t *testing.T) {
	tests := []struct {
		name        string
		field       string
		errContains string
	}{
		{
			name:        "flat resource_ prefix",
			field:       "resource_k8s.cluster",
			errContains: `resources['k8s.cluster']`,
		},
		{
			name:        "resource_service.name should suggest ServiceName",
			field:       "resource_service.name",
			errContains: `"ServiceName"`,
		},
		{
			name:        "flat event_ prefix",
			field:       "event_exception.type",
			errContains: `events['exception.type']`,
		},
		{
			name:        "SpanAttributes dot notation",
			field:       "SpanAttributes.http.method",
			errContains: `attributes['http.method']`,
		},
		{
			name:        "ResourceAttributes dot notation",
			field:       "ResourceAttributes.k8s.cluster",
			errContains: `resources['k8s.cluster']`,
		},
		{
			name:        "double-quoted bracket syntax attributes",
			field:       `attributes["http.method"]`,
			errContains: `single quotes`,
		},
		{
			name:        "double-quoted bracket syntax events",
			field:       `events["exception.type"]`,
			errContains: `single quotes`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := sanitizeTraceJSONQuery([]map[string]interface{}{
				{"type": "filter", "query": map[string]interface{}{
					"$eq": []interface{}{tt.field, "value"},
				}},
			})
			if err == nil {
				t.Fatalf("expected error for field %q, got nil", tt.field)
			}
			if !strings.Contains(err.Error(), tt.errContains) {
				t.Errorf("expected error to contain %q, got: %v", tt.errContains, err)
			}
		})
	}
}

func TestSanitizeTraceJSONQuery_EdgeCases(t *testing.T) {
	t.Run("empty pipeline passes", func(t *testing.T) {
		if err := sanitizeTraceJSONQuery([]map[string]interface{}{}); err != nil {
			t.Errorf("expected no error for empty pipeline, got: %v", err)
		}
	})

	t.Run("aggregate with no aggregates key passes (upstream validates)", func(t *testing.T) {
		stages := []map[string]interface{}{
			{"type": "aggregate"},
		}
		if err := sanitizeTraceJSONQuery(stages); err != nil {
			t.Errorf("missing aggregates key should pass sanitizer (upstream validates), got: %v", err)
		}
	})

	t.Run("aggregate entry with non-slice aggregates passes", func(t *testing.T) {
		stages := []map[string]interface{}{
			{"type": "aggregate", "aggregates": "bad"},
		}
		if err := sanitizeTraceJSONQuery(stages); err != nil {
			t.Errorf("non-slice aggregates should pass sanitizer (upstream validates type), got: %v", err)
		}
	})

	t.Run("aggregate entry that is not a map passes", func(t *testing.T) {
		stages := []map[string]interface{}{
			{
				"type":       "aggregate",
				"aggregates": []interface{}{"not_a_map"},
			},
		}
		if err := sanitizeTraceJSONQuery(stages); err != nil {
			t.Errorf("non-map aggregate entry should pass sanitizer, got: %v", err)
		}
	})
}

func TestSanitizeTraceJSONQuery_WrapsTopLevelFilterInAnd(t *testing.T) {
	t.Run("bare single field operator is wrapped in $and", func(t *testing.T) {
		stages := []map[string]interface{}{
			{"type": "filter", "query": map[string]interface{}{
				"$eq": []interface{}{"SpanKind", "SPAN_KIND_INTERNAL"},
			}},
		}
		if err := sanitizeTraceJSONQuery(stages); err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		want := map[string]interface{}{
			"$and": []interface{}{
				map[string]interface{}{"$eq": []interface{}{"SpanKind", "SPAN_KIND_INTERNAL"}},
			},
		}
		if got := stages[0]["query"]; !reflect.DeepEqual(got, want) {
			t.Errorf("query not wrapped in $and:\n got: %#v\nwant: %#v", got, want)
		}
	})

	t.Run("where stage is wrapped too", func(t *testing.T) {
		stages := []map[string]interface{}{
			{"type": "where", "query": map[string]interface{}{
				"$eq": []interface{}{"ServiceName", "checkout"},
			}},
		}
		if err := sanitizeTraceJSONQuery(stages); err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		query, ok := stages[0]["query"].(map[string]interface{})
		if !ok {
			t.Fatalf("query is not a map: %#v", stages[0]["query"])
		}
		if _, ok := query["$and"]; !ok {
			t.Errorf("expected $and wrapper, got: %#v", query)
		}
	})

	t.Run("multiple bare field operators are each wrapped in $and (sorted)", func(t *testing.T) {
		stages := []map[string]interface{}{
			{"type": "filter", "query": map[string]interface{}{
				"$eq":  []interface{}{"ServiceName", "checkout"},
				"$neq": []interface{}{"StatusCode", "STATUS_CODE_OK"},
			}},
		}
		if err := sanitizeTraceJSONQuery(stages); err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		want := map[string]interface{}{
			"$and": []interface{}{
				map[string]interface{}{"$eq": []interface{}{"ServiceName", "checkout"}},
				map[string]interface{}{"$neq": []interface{}{"StatusCode", "STATUS_CODE_OK"}},
			},
		}
		if got := stages[0]["query"]; !reflect.DeepEqual(got, want) {
			t.Errorf("multiple conditions not wrapped deterministically:\n got: %#v\nwant: %#v", got, want)
		}
	})

	t.Run("already-wrapped $and is left unchanged", func(t *testing.T) {
		inner := []interface{}{
			map[string]interface{}{"$eq": []interface{}{"SpanKind", "SPAN_KIND_INTERNAL"}},
		}
		stages := []map[string]interface{}{
			{"type": "filter", "query": map[string]interface{}{"$and": inner}},
		}
		if err := sanitizeTraceJSONQuery(stages); err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		want := map[string]interface{}{"$and": inner}
		if got := stages[0]["query"]; !reflect.DeepEqual(got, want) {
			t.Errorf("already-wrapped query should be unchanged:\n got: %#v\nwant: %#v", got, want)
		}
	})

	t.Run("top-level $or is left unchanged", func(t *testing.T) {
		or := []interface{}{
			map[string]interface{}{"$eq": []interface{}{"SpanKind", "SPAN_KIND_CLIENT"}},
			map[string]interface{}{"$eq": []interface{}{"SpanKind", "SPAN_KIND_SERVER"}},
		}
		stages := []map[string]interface{}{
			{"type": "filter", "query": map[string]interface{}{"$or": or}},
		}
		if err := sanitizeTraceJSONQuery(stages); err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		query := stages[0]["query"].(map[string]interface{})
		if _, ok := query["$or"]; !ok {
			t.Errorf("top-level $or should be preserved, got: %#v", query)
		}
		if _, ok := query["$and"]; ok {
			t.Errorf("top-level $or should not be re-wrapped in $and, got: %#v", query)
		}
	})

	t.Run("empty query map is left unchanged", func(t *testing.T) {
		stages := []map[string]interface{}{
			{"type": "filter", "query": map[string]interface{}{}},
		}
		if err := sanitizeTraceJSONQuery(stages); err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if got := stages[0]["query"]; !reflect.DeepEqual(got, map[string]interface{}{}) {
			t.Errorf("empty query should be unchanged, got: %#v", got)
		}
	})
}
