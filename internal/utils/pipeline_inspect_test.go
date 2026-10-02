package utils

import "testing"

func TestPipelineHasAggregateStage(t *testing.T) {
	tests := []struct {
		name     string
		pipeline []map[string]interface{}
		want     bool
	}{
		{name: "nil pipeline", pipeline: nil, want: false},
		{name: "empty pipeline", pipeline: []map[string]interface{}{}, want: false},
		{name: "aggregate stage", pipeline: []map[string]interface{}{{"type": "aggregate"}}, want: true},
		{name: "window_aggregate stage", pipeline: []map[string]interface{}{{"type": "window_aggregate"}}, want: true},
		{name: "filter-only pipeline", pipeline: []map[string]interface{}{{"type": "filter"}}, want: false},
		{
			name: "aggregate mixed with filter",
			pipeline: []map[string]interface{}{
				{"type": "filter"},
				{"type": "aggregate"},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PipelineHasAggregateStage(tt.pipeline); got != tt.want {
				t.Errorf("PipelineHasAggregateStage(%v) = %v, want %v", tt.pipeline, got, tt.want)
			}
		})
	}
}

// TestConditionReferencesBodyLogicalShapes locks the inspectors' handling of
// logical operators in both shapes: the sanitized array form and the map form
// that unsanitized pipelines may carry (e.g. {"$not": {…}}). Map-form values
// must be recursed into, not silently skipped (issue #241).
func TestConditionReferencesBodyLogicalShapes(t *testing.T) {
	bodyCond := map[string]any{"$contains": []any{"Body", "timeout"}}
	nonBodyCond := map[string]any{"$eq": []any{"ServiceName", "orders"}}

	cases := []struct {
		name      string
		condition map[string]any
		want      bool
	}{
		{"array-form $not on Body", map[string]any{"$not": []any{bodyCond}}, true},
		{"map-form $not on Body", map[string]any{"$not": bodyCond}, true},
		{"map-form $not without Body", map[string]any{"$not": nonBodyCond}, false},
		{"map-form $not nested in $and", map[string]any{"$and": []any{map[string]any{"$not": bodyCond}}}, true},
		{"scalar $not is skipped", map[string]any{"$not": "bogus"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := conditionReferencesBody(c.condition); got != c.want {
				t.Errorf("conditionReferencesBody = %v, want %v", got, c.want)
			}
			gotOps := collectBodyConditions(c.condition)
			if (len(gotOps) > 0) != c.want {
				t.Errorf("collectBodyConditions returned %v, want body ops present = %v", gotOps, c.want)
			}
		})
	}
}

// TestHasParseStage locks the unexported helper's contract directly.
func TestHasParseStage(t *testing.T) {
	cases := []struct {
		name     string
		pipeline []map[string]interface{}
		want     bool
	}{
		{"nil pipeline", nil, false},
		{"filter only", []map[string]interface{}{{"type": "filter"}}, false},
		{"has parse", []map[string]interface{}{{"type": "filter"}, {"type": "parse", "parser": "json"}}, true},
		{"aggregate only", []map[string]interface{}{{"type": "aggregate"}}, false},
	}
	for _, c := range cases {
		if got := hasParseStage(c.pipeline); got != c.want {
			t.Errorf("hasParseStage(%s) = %v, want %v", c.name, got, c.want)
		}
	}
}
