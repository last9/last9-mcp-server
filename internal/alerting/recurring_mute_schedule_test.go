package alerting

import (
	"strings"
	"testing"
)

func muteSchedule(tz string, windows ...RecurringMuteWindow) *RecurringMuteSchedule {
	return &RecurringMuteSchedule{Enabled: true, Timezone: tz, Windows: windows}
}

func TestValidateRecurringMuteSchedule(t *testing.T) {
	ok := RecurringMuteWindow{Weekdays: []string{"mon"}, StartTime: "09:00", EndTime: "17:30"}
	many := make([]RecurringMuteWindow, 21)
	for i := range many {
		many[i] = ok
	}
	tests := []struct {
		name    string
		s       *RecurringMuteSchedule
		wantErr string
	}{
		{"valid", muteSchedule("Asia/Kolkata", ok), ""},
		{"overnight valid", muteSchedule("UTC", RecurringMuteWindow{Weekdays: []string{"fri"}, StartTime: "22:00", EndTime: "06:00"}), ""},
		{"uppercase weekday ok", muteSchedule("UTC", RecurringMuteWindow{Weekdays: []string{"MON"}, StartTime: "00:00", EndTime: "23:59"}), ""},
		{"missing timezone", muteSchedule("", ok), "timezone is required"},
		{"bad timezone", muteSchedule("Mars/Base", ok), "not a valid IANA timezone"},
		{"no windows", muteSchedule("UTC"), "windows must not be empty"},
		{"too many windows", muteSchedule("UTC", many...), "at most 20"},
		{"no weekdays", muteSchedule("UTC", RecurringMuteWindow{StartTime: "09:00", EndTime: "10:00"}), "weekdays must not be empty"},
		{"too many weekdays", muteSchedule("UTC", RecurringMuteWindow{Weekdays: []string{"mon", "tue", "wed", "thu", "fri", "sat", "sun", "mon"}, StartTime: "09:00", EndTime: "10:00"}), "at most 7 weekdays"},
		{"bad weekday", muteSchedule("UTC", RecurringMuteWindow{Weekdays: []string{"monday"}, StartTime: "09:00", EndTime: "10:00"}), "invalid weekday"},
		{"bad start format", muteSchedule("UTC", RecurringMuteWindow{Weekdays: []string{"mon"}, StartTime: "9", EndTime: "10:00"}), "invalid start_time"},
		{"hour out of range", muteSchedule("UTC", RecurringMuteWindow{Weekdays: []string{"mon"}, StartTime: "09:00", EndTime: "24:00"}), "invalid end_time"},
		{"minute out of range", muteSchedule("UTC", RecurringMuteWindow{Weekdays: []string{"mon"}, StartTime: "09:60", EndTime: "10:00"}), "invalid start_time"},
		{"start equals end", muteSchedule("UTC", RecurringMuteWindow{Weekdays: []string{"mon"}, StartTime: "09:00", EndTime: "09:00"}), "must differ"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRecurringMuteSchedule(tt.s)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}
