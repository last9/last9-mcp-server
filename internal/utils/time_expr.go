package utils

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// timeZoneAbbreviations maps the abbreviations users type to the zone they
// most often mean. A zone keeps its DST rules: PST in July resolves to PDT.
var timeZoneAbbreviations = map[string]string{
	"UTC":  "UTC",
	"GMT":  "UTC",
	"Z":    "UTC",
	"IST":  "Asia/Kolkata",
	"PST":  "America/Los_Angeles",
	"PDT":  "America/Los_Angeles",
	"PT":   "America/Los_Angeles",
	"MST":  "America/Denver",
	"MDT":  "America/Denver",
	"MT":   "America/Denver",
	"CDT":  "America/Chicago",
	"CT":   "America/Chicago",
	"EST":  "America/New_York",
	"EDT":  "America/New_York",
	"ET":   "America/New_York",
	"BST":  "Europe/London",
	"CET":  "Europe/Berlin",
	"CEST": "Europe/Berlin",
	"JST":  "Asia/Tokyo",
	"SGT":  "Asia/Singapore",
	"AEST": "Australia/Sydney",
	"AEDT": "Australia/Sydney",
}

// ambiguousTimeZones are abbreviations common for zones hours apart; guessing
// one would silently shift every bound, so they are rejected with the choices.
var ambiguousTimeZones = map[string]string{
	"CST": "America/Chicago (US Central) or Asia/Shanghai (China)",
}

// ResolveTimeZone resolves an IANA zone name or a known abbreviation (IST →
// Asia/Kolkata). Empty means UTC.
func ResolveTimeZone(name string) (*time.Location, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return time.UTC, nil
	}
	if choices, ok := ambiguousTimeZones[strings.ToUpper(name)]; ok {
		return nil, fmt.Errorf("ambiguous timezone %q: use %s", name, choices)
	}
	if iana, ok := timeZoneAbbreviations[strings.ToUpper(name)]; ok {
		name = iana
	}
	if !strings.Contains(name, "/") && name != "UTC" {
		return nil, unknownTimeZoneError(name)
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, unknownTimeZoneError(name)
	}
	return loc, nil
}

func unknownTimeZoneError(name string) error {
	return fmt.Errorf("unknown timezone %q: use an IANA name such as Asia/Kolkata or an abbreviation such as IST, UTC, PST, EST", name)
}

func isTimeZoneToken(tok string) bool {
	_, known := timeZoneAbbreviations[strings.ToUpper(tok)]
	_, ambiguous := ambiguousTimeZones[strings.ToUpper(tok)]
	return known || ambiguous || strings.Contains(tok, "/")
}

// ParseTimeRangeExpr resolves a range expression against now. Calendar words
// (today, yesterday, 9am) are read in loc unless the expression ends in its own
// zone. Accepted forms:
//
//	last|past <n><unit> | <n> <unit> | <unit>      e.g. last 2h, past 30 minutes
//	today | yesterday
//	since <point>                                  e.g. since yesterday 9am IST
//	<point> to <point>
//
// A point is an RFC3339 timestamp, or [today|yesterday|<weekday>|YYYY-MM-DD]
// [<clock>], where clock is 9am, 9:30pm, 9 am or 14:30.
func ParseTimeRangeExpr(expr string, now time.Time, loc *time.Location) (start, end time.Time, err error) {
	tokens := strings.Fields(expr)
	if n := len(tokens); n > 1 && isTimeZoneToken(tokens[n-1]) {
		if loc, err = ResolveTimeZone(tokens[n-1]); err != nil {
			return time.Time{}, time.Time{}, err
		}
		tokens = tokens[:n-1]
	}
	start, end, err = parseRangeTokens(tokens, now, loc)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	if !start.Before(end) {
		return time.Time{}, time.Time{}, fmt.Errorf("range %q starts after it ends (%s ≥ %s)",
			expr, start.UTC().Format(time.RFC3339), end.UTC().Format(time.RFC3339))
	}
	return start.UTC(), end.UTC(), nil
}

func parseRangeTokens(tokens []string, now time.Time, loc *time.Location) (start, end time.Time, err error) {
	if len(tokens) == 0 {
		return time.Time{}, time.Time{}, fmt.Errorf("range is empty")
	}
	for i, tok := range tokens {
		if strings.EqualFold(tok, "to") {
			if start, err = parsePoint(tokens[:i], now, loc); err != nil {
				return time.Time{}, time.Time{}, err
			}
			end, err = parsePoint(tokens[i+1:], now, loc)
			return start, end, err
		}
	}
	head := strings.ToLower(tokens[0])
	switch {
	case head == "last" || head == "past":
		d, err := parseLookback(tokens[1:])
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
		return now.Add(-d), now, nil
	case head == "since":
		start, err = parsePoint(tokens[1:], now, loc)
		return start, now, err
	case len(tokens) == 1 && head == "today":
		return startOfDay(now, loc, 0), now, nil
	case len(tokens) == 1 && head == "yesterday":
		return startOfDay(now, loc, -1), startOfDay(now, loc, 0), nil
	}
	return time.Time{}, time.Time{}, fmt.Errorf(
		"unsupported range %q: use last <n><unit>, today, yesterday, since <point>, or <point> to <point>",
		strings.Join(tokens, " "))
}

var lookbackUnits = map[string]time.Duration{
	"s": time.Second, "sec": time.Second, "secs": time.Second, "second": time.Second, "seconds": time.Second,
	"m": time.Minute, "min": time.Minute, "mins": time.Minute, "minute": time.Minute, "minutes": time.Minute,
	"h": time.Hour, "hr": time.Hour, "hrs": time.Hour, "hour": time.Hour, "hours": time.Hour,
	"d": 24 * time.Hour, "day": 24 * time.Hour, "days": 24 * time.Hour,
	"w": 7 * 24 * time.Hour, "wk": 7 * 24 * time.Hour, "wks": 7 * 24 * time.Hour, "week": 7 * 24 * time.Hour, "weeks": 7 * 24 * time.Hour,
}

var compactLookback = regexp.MustCompile(`^(\d+)([a-z]+)$`)

// parseLookback reads "2h", "2 hours", "hour" or a Go duration such as "1h30m".
// Days and weeks are fixed 24h and 168h spans, not calendar days.
func parseLookback(tokens []string) (time.Duration, error) {
	var count, unit string
	switch len(tokens) {
	case 1:
		tok := strings.ToLower(tokens[0])
		if m := compactLookback.FindStringSubmatch(tok); m != nil {
			count, unit = m[1], m[2]
		} else if _, ok := lookbackUnits[tok]; ok {
			count, unit = "1", tok
		} else if d, err := time.ParseDuration(tok); err == nil {
			return positiveLookback(d)
		} else {
			return 0, fmt.Errorf("unrecognized duration %q: use forms like 2h, 30 minutes or 7d", tokens[0])
		}
	case 2:
		count, unit = tokens[0], strings.ToLower(tokens[1])
	default:
		return 0, fmt.Errorf("last needs a duration such as 2h, 30 minutes or 7d")
	}
	n, err := strconv.Atoi(count)
	if err != nil {
		return 0, fmt.Errorf("unrecognized duration %q: use forms like 2h, 30 minutes or 7d", count+" "+unit)
	}
	per, ok := lookbackUnits[unit]
	if !ok {
		return 0, fmt.Errorf("unknown unit %q: use s, m, h, d or w", unit)
	}
	return positiveLookback(time.Duration(n) * per)
}

func positiveLookback(d time.Duration) (time.Duration, error) {
	if d <= 0 {
		return 0, fmt.Errorf("duration must be positive")
	}
	return d, nil
}

// parsePoint reads an RFC3339 timestamp (via ParseToolTimestamp) or a calendar
// day with an optional clock time in loc.
func parsePoint(tokens []string, now time.Time, loc *time.Location) (time.Time, error) {
	if len(tokens) == 0 {
		return time.Time{}, fmt.Errorf("expected a point in time such as yesterday 9am or 2026-02-09T15:04:05Z")
	}
	if len(tokens) == 1 {
		if ts, err := ParseToolTimestamp(tokens[0]); err == nil {
			return ts, nil
		}
	}
	day := startOfDay(now, loc, 0)
	clock := tokens
	switch head := strings.ToLower(tokens[0]); {
	case head == "today":
		clock = tokens[1:]
	case head == "yesterday":
		day, clock = startOfDay(now, loc, -1), tokens[1:]
	case isWeekday(head):
		day, clock = latestWeekday(now, loc, weekdays[head]), tokens[1:]
	default:
		if d, err := time.ParseInLocation("2006-01-02", tokens[0], loc); err == nil {
			day, clock = d, tokens[1:]
		}
	}
	if len(clock) == 0 {
		return day, nil
	}
	hour, minute, err := parseClock(strings.ToLower(strings.Join(clock, "")))
	if err != nil {
		return time.Time{}, err
	}
	return time.Date(day.Year(), day.Month(), day.Day(), hour, minute, 0, 0, loc), nil
}

var clockPattern = regexp.MustCompile(`^(\d{1,2})(?::(\d{2}))?(am|pm)?$`)

func parseClock(s string) (hour, minute int, err error) {
	m := clockPattern.FindStringSubmatch(s)
	if m == nil {
		return 0, 0, fmt.Errorf("unrecognized time of day %q: use 9am, 9:30pm or 14:30", s)
	}
	hour, _ = strconv.Atoi(m[1])
	if m[2] != "" {
		minute, _ = strconv.Atoi(m[2])
	}
	switch meridiem := m[3]; {
	case meridiem == "" && m[2] == "":
		return 0, 0, fmt.Errorf("unrecognized time of day %q: use 9am, 9:30pm or 14:30", s)
	case meridiem == "" && hour > 23, meridiem != "" && (hour < 1 || hour > 12), minute > 59:
		return 0, 0, fmt.Errorf("invalid time of day %q", s)
	case meridiem == "am" && hour == 12:
		hour = 0
	case meridiem == "pm" && hour != 12:
		hour += 12
	}
	return hour, minute, nil
}

var weekdays = map[string]time.Weekday{
	"sunday": time.Sunday, "sun": time.Sunday,
	"monday": time.Monday, "mon": time.Monday,
	"tuesday": time.Tuesday, "tue": time.Tuesday,
	"wednesday": time.Wednesday, "wed": time.Wednesday,
	"thursday": time.Thursday, "thu": time.Thursday,
	"friday": time.Friday, "fri": time.Friday,
	"saturday": time.Saturday, "sat": time.Saturday,
}

func isWeekday(s string) bool {
	_, ok := weekdays[s]
	return ok
}

// latestWeekday returns local midnight of the most recent day named wd, today
// included: on a Monday, "since monday" means since this morning.
func latestWeekday(now time.Time, loc *time.Location, wd time.Weekday) time.Time {
	back := (int(now.In(loc).Weekday()) - int(wd) + 7) % 7
	return startOfDay(now, loc, -back)
}

// startOfDay returns local midnight offset by days. time.Date normalizes, so a
// day that DST shortens or lengthens keeps its true 23h or 25h span.
func startOfDay(now time.Time, loc *time.Location, days int) time.Time {
	local := now.In(loc)
	return time.Date(local.Year(), local.Month(), local.Day()+days, 0, 0, 0, 0, loc)
}
