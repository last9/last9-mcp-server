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
