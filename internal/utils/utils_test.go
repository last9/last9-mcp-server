package utils

import (
	"testing"
	"time"
)

func TestGetTimeRange_TimezoneHandling(t *testing.T) {
	tests := []struct {
		name          string
		params        map[string]interface{}
		wantStartUnix int64
		wantEndUnix   int64
		wantErr       bool
	}{
		{
			name: "legacy ISO timestamps parsed as UTC",
			params: map[string]interface{}{
				"start_time_iso": "2025-06-23 16:00:00",
				"end_time_iso":   "2025-06-23 16:30:00",
			},
			wantStartUnix: 1750694400, // 2025-06-23 16:00:00 UTC
			wantEndUnix:   1750696200, // 2025-06-23 16:30:00 UTC
			wantErr:       false,
		},
		{
			name: "RFC3339 timestamps parsed as UTC",
			params: map[string]interface{}{
				"start_time_iso": "2025-06-23T16:00:00Z",
				"end_time_iso":   "2025-06-23T16:30:00Z",
			},
			wantStartUnix: 1750694400,
			wantEndUnix:   1750696200,
			wantErr:       false,
		},
		{
			name: "only start_time provided - end time should be start + lookback",
			params: map[string]interface{}{
				"start_time_iso": "2025-06-27 16:00:00",
			},
			wantStartUnix: 1751040000, // 2025-06-27 16:00:00 UTC
			// end time should be start + lookback and is checked separately
			wantErr: false,
		},
		{
			name: "only end_time provided - start time should be end - lookback",
			params: map[string]interface{}{
				"end_time_iso": "2025-06-27 16:00:00",
			},
			wantEndUnix: 1751040000, // 2025-06-27 16:00:00 UTC
			wantErr:     false,
		},
		{
			name: "lookback minutes only - no explicit timestamps",
			params: map[string]interface{}{
				"lookback_minutes": float64(30),
			},
			// timestamps will be calculated from current time
			wantErr: false,
		},
		{
			name: "invalid start_time format",
			params: map[string]interface{}{
				"start_time_iso": "invalid-time",
			},
			wantErr: true,
		},
		{
			name: "invalid end_time format",
			params: map[string]interface{}{
				"end_time_iso": "invalid-time",
			},
			wantErr: true,
		},
		{
			name: "start_time after end_time",
			params: map[string]interface{}{
				"start_time_iso": "2025-06-23 17:00:00",
				"end_time_iso":   "2025-06-23 16:00:00",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end, err := GetTimeRange(tt.params, 60)

			if tt.wantErr {
				if err == nil {
					t.Errorf("GetTimeRange() expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("GetTimeRange() unexpected error: %v", err)
				return
			}

			// Check start time if specified
			if tt.wantStartUnix != 0 {
				if start.Unix() != tt.wantStartUnix {
					t.Errorf("GetTimeRange() start time = %d, want %d", start.Unix(), tt.wantStartUnix)
				}
			}

			// Check end time if specified
			if tt.wantEndUnix != 0 {
				if end.Unix() != tt.wantEndUnix {
					t.Errorf("GetTimeRange() end time = %d, want %d", end.Unix(), tt.wantEndUnix)
				}
			}

			// Special checks for edge cases
			if tt.name == "only start_time provided - end time should be start + lookback" {
				// End time should be start time + 60 minutes (default lookback)
				expectedEnd := start.Add(60 * time.Minute)
				if end.Unix() != expectedEnd.Unix() {
					t.Errorf("GetTimeRange() end time = %d, want %d (start + 60min)", end.Unix(), expectedEnd.Unix())
				}
			}

			if tt.name == "only end_time provided - start time should be end - lookback" {
				// Start time should be end time - 60 minutes (default lookback)
				expectedStart := end.Add(-60 * time.Minute)
				if start.Unix() != expectedStart.Unix() {
					t.Errorf("GetTimeRange() start time = %d, want %d (end - 60min)", start.Unix(), expectedStart.Unix())
				}
			}

			if tt.name == "lookback minutes only - no explicit timestamps" {
				// Should use current time with 30 minute lookback
				now := time.Now().UTC()
				timeDiff := end.Sub(now)
				if timeDiff < -5*time.Second || timeDiff > 5*time.Second {
					t.Errorf("GetTimeRange() end time should be close to now, got diff: %v", timeDiff)
				}
				expectedDiff := 30 * time.Minute
				actualDiff := end.Sub(start)
				if actualDiff != expectedDiff {
					t.Errorf("GetTimeRange() time difference = %v, want %v", actualDiff, expectedDiff)
				}
			}
		})
	}
}

func TestGetTimeRange_LookbackMinutes(t *testing.T) {
	tests := []struct {
		name                   string
		params                 map[string]interface{}
		defaultLookbackMinutes int
		wantLookbackUsed       int
		wantErr                bool
	}{
		{
			name:                   "default lookback minutes",
			params:                 map[string]interface{}{},
			defaultLookbackMinutes: 30,
			wantLookbackUsed:       30,
			wantErr:                false,
		},
		{
			name: "custom lookback minutes",
			params: map[string]interface{}{
				"lookback_minutes": float64(45),
			},
			defaultLookbackMinutes: 30,
			wantLookbackUsed:       45,
			wantErr:                false,
		},
		{
			name: "lookback too small",
			params: map[string]interface{}{
				"lookback_minutes": float64(0),
			},
			defaultLookbackMinutes: 30,
			wantErr:                true,
		},
		{
			name: "lookback too large",
			params: map[string]interface{}{
				"lookback_minutes": float64(1500), // > 1440
			},
			defaultLookbackMinutes: 30,
			wantErr:                true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end, err := GetTimeRange(tt.params, tt.defaultLookbackMinutes)

			if tt.wantErr {
				if err == nil {
					t.Errorf("GetTimeRange() expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("GetTimeRange() unexpected error: %v", err)
				return
			}

			// Check that the time difference matches expected lookback
			timeDiff := end.Sub(start)
			expectedDiff := time.Duration(tt.wantLookbackUsed) * time.Minute

			if timeDiff != expectedDiff {
				t.Errorf("GetTimeRange() time difference = %v, want %v", timeDiff, expectedDiff)
			}

			// Verify times are in UTC
			if start.Location() != time.UTC {
				t.Errorf("GetTimeRange() start time not in UTC: %v", start.Location())
			}
			if end.Location() != time.UTC {
				t.Errorf("GetTimeRange() end time not in UTC: %v", end.Location())
			}
		})
	}
}

func TestGetTimeRange_UTCConsistency(t *testing.T) {
	// Test that all returned times are consistently in UTC
	params := map[string]interface{}{
		"start_time_iso": "2025-06-23T16:00:00Z",
		"end_time_iso":   "2025-06-23T16:30:00Z",
	}

	start, end, err := GetTimeRange(params, 60)
	if err != nil {
		t.Fatalf("GetTimeRange() unexpected error: %v", err)
	}

	if start.Location() != time.UTC {
		t.Errorf("Start time not in UTC timezone: %v", start.Location())
	}

	if end.Location() != time.UTC {
		t.Errorf("End time not in UTC timezone: %v", end.Location())
	}

	// Test the actual scenario from the user's bug report
	// Input: 2025-06-23 16:00:00 should return Unix timestamp 1750694400
	expectedUnixStart := int64(1750694400)
	if start.Unix() != expectedUnixStart {
		t.Errorf("Unix timestamp mismatch: got %d, want %d", start.Unix(), expectedUnixStart)
	}
}

func TestGetTimeRange_TimeRangeValidation(t *testing.T) {
	tests := []struct {
		name    string
		params  map[string]interface{}
		wantErr bool
		errMsg  string
	}{
		{
			name: "time range exceeds 24 hours",
			params: map[string]interface{}{
				"start_time_iso": "2025-06-23 00:00:00",
				"end_time_iso":   "2025-06-24 01:00:00", // 25 hours
			},
			wantErr: true,
			errMsg:  "time range cannot exceed 24 hours",
		},
		{
			name: "valid 24 hour range",
			params: map[string]interface{}{
				"start_time_iso": "2025-06-23 00:00:00",
				"end_time_iso":   "2025-06-24 00:00:00", // exactly 24 hours
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := GetTimeRange(tt.params, 60)

			if tt.wantErr {
				if err == nil {
					t.Errorf("GetTimeRange() expected error but got none")
					return
				}
				if tt.errMsg != "" && err.Error() != tt.errMsg {
					t.Errorf("GetTimeRange() error = %q, want %q", err.Error(), tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("GetTimeRange() unexpected error: %v", err)
				}
			}
		})
	}
}
