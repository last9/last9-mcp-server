package traces

import "testing"

func TestParseTimeRangeFromArgs(t *testing.T) {
	tests := []struct {
		name    string
		args    GetTracesArgs
		wantErr bool
	}{
		{
			name: "valid RFC3339 range",
			args: GetTracesArgs{
				StartTimeISO: "2026-02-09T15:04:05Z",
				EndTimeISO:   "2026-02-09T15:34:05Z",
			},
			wantErr: false,
		},
		{
			name: "valid legacy range compatibility",
			args: GetTracesArgs{
				StartTimeISO: "2026-02-09 15:04:05",
				EndTimeISO:   "2026-02-09 15:34:05",
			},
			wantErr: false,
		},
		{
			name: "invalid start_time_iso should fail",
			args: GetTracesArgs{
				StartTimeISO: "2026/02/09 15:04:05",
			},
			wantErr: true,
		},
		{
			name: "invalid end_time_iso should fail",
			args: GetTracesArgs{
				EndTimeISO: "not-a-time",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := parseTimeRangeFromArgs(tt.args)
			if tt.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
