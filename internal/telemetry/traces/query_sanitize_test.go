package traces

import (
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
