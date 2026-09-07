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
