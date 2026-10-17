package utils

import (
	"strings"
	"testing"
	"time"
)

func mustLoad(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func TestResolveTimeZone(t *testing.T) {
	tests := []struct {
		name    string
		want    string
		wantErr string
	}{
		{name: "", want: "UTC"},
		{name: "  ", want: "UTC"},
		{name: "UTC", want: "UTC"},
		{name: "utc", want: "UTC"},
		{name: "GMT", want: "UTC"},
		{name: "IST", want: "Asia/Kolkata"},
		{name: "ist", want: "Asia/Kolkata"},
		{name: "PST", want: "America/Los_Angeles"},
		{name: "EDT", want: "America/New_York"},
		{name: "CDT", want: "America/Chicago"},
		{name: "CT", want: "America/Chicago"},
		{name: "CST", wantErr: `ambiguous timezone "CST": use America/Chicago (US Central) or Asia/Shanghai (China)`},
		{name: "cst", wantErr: `ambiguous timezone "cst"`},
		{name: "Asia/Kolkata", want: "Asia/Kolkata"},
		{name: "America/New_York", want: "America/New_York"},
		{name: "Mars/Olympus", wantErr: `unknown timezone "Mars/Olympus"`},
		{name: "XYZ", wantErr: `unknown timezone "XYZ"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loc, err := ResolveTimeZone(tt.name)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if loc.String() != tt.want {
				t.Fatalf("zone = %q, want %q", loc, tt.want)
			}
		})
	}
}

func TestParseTimeRangeExpr(t *testing.T) {
	// 2026-10-16 19:00 UTC is 2026-10-17 00:30 in India: "yesterday" there is the 16th.
	now := time.Date(2026, 10, 16, 19, 0, 0, 0, time.UTC)
	utc := time.UTC
	ist := mustLoad(t, "Asia/Kolkata")
	ny := mustLoad(t, "America/New_York")
	// US DST began 2026-03-08 at 02:00 local; 11:00 EDT on the 9th.
	afterSpringForward := time.Date(2026, 3, 9, 15, 0, 0, 0, time.UTC)
	// US DST ended 2026-11-01 at 02:00 local; 10:00 EST on the 2nd.
	afterFallBack := time.Date(2026, 11, 2, 15, 0, 0, 0, time.UTC)

	at := func(s string) time.Time {
		v, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}

	tests := []struct {
		name      string
		expr      string
		now       time.Time
		loc       *time.Location
		wantStart string
		wantEnd   string
	}{
		{name: "last compact hours", expr: "last 2h", now: now, loc: utc, wantStart: "2026-10-16T17:00:00Z", wantEnd: "2026-10-16T19:00:00Z"},
		{name: "last spaced minutes", expr: "last 30 minutes", now: now, loc: utc, wantStart: "2026-10-16T18:30:00Z", wantEnd: "2026-10-16T19:00:00Z"},
		{name: "past min", expr: "past 45 min", now: now, loc: utc, wantStart: "2026-10-16T18:15:00Z", wantEnd: "2026-10-16T19:00:00Z"},
		{name: "last seconds", expr: "last 90s", now: now, loc: utc, wantStart: "2026-10-16T18:58:30Z", wantEnd: "2026-10-16T19:00:00Z"},
		{name: "last unit without count", expr: "last hour", now: now, loc: utc, wantStart: "2026-10-16T18:00:00Z", wantEnd: "2026-10-16T19:00:00Z"},
		{name: "last days", expr: "last 2 days", now: now, loc: utc, wantStart: "2026-10-14T19:00:00Z", wantEnd: "2026-10-16T19:00:00Z"},
		{name: "last week", expr: "last 1w", now: now, loc: utc, wantStart: "2026-10-09T19:00:00Z", wantEnd: "2026-10-16T19:00:00Z"},
		{name: "go duration", expr: "last 1h30m", now: now, loc: utc, wantStart: "2026-10-16T17:30:00Z", wantEnd: "2026-10-16T19:00:00Z"},
		{name: "case and spacing", expr: "  Last   2H ", now: now, loc: utc, wantStart: "2026-10-16T17:00:00Z", wantEnd: "2026-10-16T19:00:00Z"},
		{name: "today utc", expr: "today", now: now, loc: utc, wantStart: "2026-10-16T00:00:00Z", wantEnd: "2026-10-16T19:00:00Z"},
		{name: "yesterday utc", expr: "yesterday", now: now, loc: utc, wantStart: "2026-10-15T00:00:00Z", wantEnd: "2026-10-16T00:00:00Z"},
		{name: "today in local zone", expr: "today", now: now, loc: ist, wantStart: "2026-10-16T18:30:00Z", wantEnd: "2026-10-16T19:00:00Z"},
		{name: "since yesterday 9am inline IST", expr: "since yesterday 9am IST", now: now, loc: utc, wantStart: "2026-10-16T03:30:00Z", wantEnd: "2026-10-16T19:00:00Z"},
		{name: "since yesterday 9am zone param", expr: "since yesterday 9am", now: now, loc: ist, wantStart: "2026-10-16T03:30:00Z", wantEnd: "2026-10-16T19:00:00Z"},
		{name: "inline zone beats param", expr: "since yesterday 9am UTC", now: now, loc: ist, wantStart: "2026-10-15T09:00:00Z", wantEnd: "2026-10-16T19:00:00Z"},
		{name: "since today 24h clock", expr: "since today 14:30", now: now, loc: utc, wantStart: "2026-10-16T14:30:00Z", wantEnd: "2026-10-16T19:00:00Z"},
		{name: "since time only is today", expr: "since 12:15am IST", now: now, loc: utc, wantStart: "2026-10-16T18:45:00Z", wantEnd: "2026-10-16T19:00:00Z"},
		{name: "since spaced meridiem", expr: "since yesterday 9 am", now: now, loc: utc, wantStart: "2026-10-15T09:00:00Z", wantEnd: "2026-10-16T19:00:00Z"},
		{name: "since 12am is midnight", expr: "since today 12am", now: now, loc: utc, wantStart: "2026-10-16T00:00:00Z", wantEnd: "2026-10-16T19:00:00Z"},
		{name: "since 12pm is noon", expr: "since today 12pm", now: now, loc: utc, wantStart: "2026-10-16T12:00:00Z", wantEnd: "2026-10-16T19:00:00Z"},
		{name: "since date", expr: "since 2026-10-14", now: now, loc: utc, wantStart: "2026-10-14T00:00:00Z", wantEnd: "2026-10-16T19:00:00Z"},
		{name: "since date and time IANA", expr: "since 2026-10-14 08:00 America/New_York", now: now, loc: utc, wantStart: "2026-10-14T12:00:00Z", wantEnd: "2026-10-16T19:00:00Z"},
		{name: "since ISO timestamp", expr: "since 2026-10-16T10:00:00+05:30", now: now, loc: utc, wantStart: "2026-10-16T04:30:00Z", wantEnd: "2026-10-16T19:00:00Z"},
		{name: "absolute ISO to ISO", expr: "2026-10-16T10:00:00Z to 2026-10-16T11:00:00Z", now: now, loc: utc, wantStart: "2026-10-16T10:00:00Z", wantEnd: "2026-10-16T11:00:00Z"},
		{name: "day times to with shared zone", expr: "yesterday 9am to yesterday 11am IST", now: now, loc: utc, wantStart: "2026-10-16T03:30:00Z", wantEnd: "2026-10-16T05:30:00Z"},
		{name: "spring forward yesterday is 23h", expr: "yesterday", now: afterSpringForward, loc: ny, wantStart: "2026-03-08T05:00:00Z", wantEnd: "2026-03-09T04:00:00Z"},
		{name: "spring forward before switch is EST", expr: "since yesterday 1am", now: afterSpringForward, loc: ny, wantStart: "2026-03-08T06:00:00Z", wantEnd: "2026-03-09T15:00:00Z"},
		{name: "spring forward after switch is EDT", expr: "since yesterday 9am", now: afterSpringForward, loc: ny, wantStart: "2026-03-08T13:00:00Z", wantEnd: "2026-03-09T15:00:00Z"},
		{name: "fall back yesterday is 25h", expr: "yesterday", now: afterFallBack, loc: ny, wantStart: "2026-11-01T04:00:00Z", wantEnd: "2026-11-02T05:00:00Z"},
		{name: "last 24h ignores DST", expr: "last 24h", now: afterFallBack, loc: ny, wantStart: "2026-11-01T15:00:00Z", wantEnd: "2026-11-02T15:00:00Z"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end, err := ParseTimeRangeExpr(tt.expr, tt.now, tt.loc)
			if err != nil {
				t.Fatalf("ParseTimeRangeExpr(%q) error = %v", tt.expr, err)
			}
			if !start.Equal(at(tt.wantStart)) || !end.Equal(at(tt.wantEnd)) {
				t.Fatalf("ParseTimeRangeExpr(%q) = [%s, %s], want [%s, %s]",
					tt.expr, start.UTC().Format(time.RFC3339), end.UTC().Format(time.RFC3339), tt.wantStart, tt.wantEnd)
			}
		})
	}
}

func TestParseTimeRangeExprErrors(t *testing.T) {
	now := time.Date(2026, 10, 16, 19, 0, 0, 0, time.UTC)
	tests := []struct {
		expr    string
		wantErr string
	}{
		{expr: "since yesterday 9am Mars/Olympus", wantErr: `unknown timezone "Mars/Olympus"`},
		{expr: "since yesterday 9am CST", wantErr: `ambiguous timezone "CST"`},
		{expr: "last", wantErr: "duration"},
		{expr: "last 0h", wantErr: "positive"},
		{expr: "last 2 fortnights", wantErr: `unknown unit "fortnights"`},
		{expr: "since", wantErr: "point in time"},
		{expr: "since today 25:00", wantErr: `"25:00"`},
		{expr: "since today 13pm", wantErr: `"13pm"`},
		{expr: "since today 11pm", wantErr: "starts after it ends"},
		{expr: "2026-10-16T11:00:00Z to 2026-10-16T10:00:00Z", wantErr: "starts after it ends"},
		{expr: "next week", wantErr: "unsupported range"},
		{expr: "yesterday 9am", wantErr: "unsupported range"},
	}
	for _, tt := range tests {
		t.Run(tt.expr, func(t *testing.T) {
			_, _, err := ParseTimeRangeExpr(tt.expr, now, time.UTC)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ParseTimeRangeExpr(%q) err = %v, want containing %q", tt.expr, err, tt.wantErr)
			}
		})
	}
}
