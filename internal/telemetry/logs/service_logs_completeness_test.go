package logs

import (
	"encoding/json"
	"testing"
)

func TestChunksFailed_ReadsEveryNumericType(t *testing.T) {
	cases := map[string]struct {
		stats   map[string]any
		want    int
		wantErr bool
	}{
		"absent stats":    {stats: nil, want: 0},
		"absent field":    {stats: map[string]any{"chunks_planned": float64(6)}, want: 0},
		"null field":      {stats: map[string]any{"chunks_failed": nil}, want: 0},
		"float64":         {stats: map[string]any{"chunks_failed": float64(2)}, want: 2},
		"int":             {stats: map[string]any{"chunks_failed": 3}, want: 3},
		"int64":           {stats: map[string]any{"chunks_failed": int64(4)}, want: 4},
		"json.Number":     {stats: map[string]any{"chunks_failed": json.Number("5")}, want: 5},
		"zero":            {stats: map[string]any{"chunks_failed": float64(0)}, want: 0},
		"string":          {stats: map[string]any{"chunks_failed": "1"}, wantErr: true},
		"bad json.Number": {stats: map[string]any{"chunks_failed": json.Number("1.5")}, wantErr: true},
		"fractional":      {stats: map[string]any{"chunks_failed": 0.5}, wantErr: true},
		"negative":        {stats: map[string]any{"chunks_failed": float64(-1)}, wantErr: true},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := (&ServiceLogsResponse{SearchStats: c.stats}).chunksFailed()
			if c.wantErr {
				if err == nil {
					t.Fatalf("got %d, want an error", got)
				}
				return
			}
			if err != nil || got != c.want {
				t.Fatalf("got %d, %v; want %d", got, err, c.want)
			}
		})
	}
}

func TestMayMissLines(t *testing.T) {
	truncated, complete := true, false
	lines := make([]LogEntry, 3)
	cases := map[string]struct {
		resp  ServiceLogsResponse
		limit int
		want  bool
	}{
		"truncated search":          {resp: ServiceLogsResponse{Logs: lines, LogsTruncated: &truncated, TotalMatchingLines: intPtr(3)}, limit: 10, want: true},
		"total above returned":      {resp: ServiceLogsResponse{Logs: lines, LogsTruncated: &complete, TotalMatchingLines: intPtr(4)}, limit: 3, want: true},
		"total equals returned":     {resp: ServiceLogsResponse{Logs: lines, LogsTruncated: &complete, TotalMatchingLines: intPtr(3)}, limit: 3, want: false},
		"no total, limit filled":    {resp: ServiceLogsResponse{Logs: lines}, limit: 3, want: true},
		"no total, below the limit": {resp: ServiceLogsResponse{Logs: lines}, limit: 4, want: false},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if got := c.resp.mayMissLines(c.limit); got != c.want {
				t.Fatalf("mayMissLines(%d) = %v, want %v", c.limit, got, c.want)
			}
		})
	}
}
