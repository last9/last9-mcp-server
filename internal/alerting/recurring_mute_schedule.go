package alerting

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	maxMuteWindows           = 20
	maxWeekdaysPerMuteWindow = 7
)

var muteWeekdays = map[string]struct{}{"sun": {}, "mon": {}, "tue": {}, "wed": {}, "thu": {}, "fri": {}, "sat": {}}

type RecurringMuteWindow struct {
	Weekdays  []string `json:"weekdays" jsonschema:"(Required) Days the window starts on: sun, mon, tue, wed, thu, fri, sat (1-7 entries)"`
	StartTime string   `json:"start_time" jsonschema:"(Required) Window start, 24-hour HH:MM in the schedule timezone"`
	EndTime   string   `json:"end_time" jsonschema:"(Required) Window end, 24-hour HH:MM in the schedule timezone; must differ from start_time. An end at or before start_time ends on the following day"`
}

type RecurringMuteSchedule struct {
	Enabled  bool                  `json:"enabled" jsonschema:"Set true to suppress notifications during the windows, false to keep the schedule stored but inactive"`
	Timezone string                `json:"timezone" jsonschema:"(Required) IANA timezone name, for example Asia/Kolkata"`
	Windows  []RecurringMuteWindow `json:"windows" jsonschema:"(Required) 1-20 weekly mute windows"`
}

func validateRecurringMuteSchedule(s *RecurringMuteSchedule) error {
	if strings.TrimSpace(s.Timezone) == "" {
		return fmt.Errorf("recurring_mute_schedule.timezone is required")
	}
	if _, err := time.LoadLocation(strings.TrimSpace(s.Timezone)); err != nil {
		return fmt.Errorf("recurring_mute_schedule.timezone %q is not a valid IANA timezone: %w", s.Timezone, err)
	}
	if len(s.Windows) == 0 {
		return fmt.Errorf("recurring_mute_schedule.windows must not be empty")
	}
	if len(s.Windows) > maxMuteWindows {
		return fmt.Errorf("recurring_mute_schedule.windows: at most %d allowed", maxMuteWindows)
	}
	for i := range s.Windows {
		if err := validateMuteWindow(&s.Windows[i]); err != nil {
			return fmt.Errorf("recurring_mute_schedule.windows[%d]: %w", i, err)
		}
	}
	return nil
}

func validateMuteWindow(w *RecurringMuteWindow) error {
	if len(w.Weekdays) > maxWeekdaysPerMuteWindow {
		return fmt.Errorf("at most %d weekdays allowed", maxWeekdaysPerMuteWindow)
	}
	seen := map[string]struct{}{}
	days := make([]string, 0, len(w.Weekdays))
	for _, d := range w.Weekdays {
		d = strings.ToLower(d)
		if _, ok := muteWeekdays[d]; !ok {
			return fmt.Errorf("invalid weekday %q: must be one of sun,mon,tue,wed,thu,fri,sat", d)
		}
		if _, dup := seen[d]; !dup {
			seen[d] = struct{}{}
			days = append(days, d)
		}
	}
	if len(days) == 0 {
		return fmt.Errorf("weekdays must not be empty")
	}
	w.Weekdays = days
	startH, startM, err := parseMuteClock(w.StartTime)
	if err != nil {
		return fmt.Errorf("invalid start_time: %w", err)
	}
	endH, endM, err := parseMuteClock(w.EndTime)
	if err != nil {
		return fmt.Errorf("invalid end_time: %w", err)
	}
	if startH == endH && startM == endM {
		return fmt.Errorf("start_time and end_time must differ")
	}
	return nil
}

func parseMuteClock(s string) (int, int, error) {
	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("expected HH:MM, got %q", s)
	}
	h, err := strconv.Atoi(parts[0])
	if err != nil || h < 0 || h > 23 {
		return 0, 0, fmt.Errorf("invalid hour in %q", s)
	}
	m, err := strconv.Atoi(parts[1])
	if err != nil || m < 0 || m > 59 {
		return 0, 0, fmt.Errorf("invalid minute in %q", s)
	}
	return h, m, nil
}
