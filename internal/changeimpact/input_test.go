package changeimpact

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"last9-mcp/internal/change_events"
)

func TestShortOrFutureAfterWindowIsInsufficientWithoutQuerying(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Minute)
	for name, change := range map[string]time.Time{
		"future":     now.Add(time.Hour),
		"3m elapsed": now.Add(-3 * time.Minute),
	} {
		t.Run(name, func(t *testing.T) {
			f := &fakeLast9{change: change}
			report, err := runTool(t, f, serviceArgs(change))
			if err != nil {
				t.Fatal(err)
			}
			if report.Verdict != VerdictInsufficientData || !strings.Contains(report.Reason, "after window") {
				t.Fatalf("verdict = %q (%s)", report.Verdict, report.Reason)
			}
			if len(f.requests) != 0 {
				t.Fatalf("made %d requests, want none", len(f.requests))
			}
			if report.GoldenSignals.Skipped == "" || report.Alerts.Skipped == "" {
				t.Fatal("skipped sections must say why")
			}
		})
	}
}

func TestInputValidationRejectsBeforeAnyRequest(t *testing.T) {
	change := recentChange()
	event := &change_events.TimeSeries{
		Metric: map[string]string{"service": "checkout"},
		Values: []change_events.TimeSeriesPoint{{Timestamp: uint64(change.Unix()), Value: 1}},
	}
	cases := map[string]struct {
		args AnalyzeChangeImpactArgs
		want string
	}{
		"neither form":          {AnalyzeChangeImpactArgs{}, "change_event or service"},
		"blank service":         {AnalyzeChangeImpactArgs{Service: "  ", Timestamp: "1700000000"}, "change_event or service"},
		"service only":          {AnalyzeChangeImpactArgs{Service: "checkout"}, "timestamp"},
		"both forms":            {AnalyzeChangeImpactArgs{ChangeEvent: event, Service: "checkout", Timestamp: "1700000000"}, "not both"},
		"event plus timestamp":  {AnalyzeChangeImpactArgs{ChangeEvent: event, Timestamp: "1700000000"}, "not both"},
		"bad timestamp":         {AnalyzeChangeImpactArgs{Service: "checkout", Timestamp: "yesterday"}, "timestamp"},
		"milliseconds":          {AnalyzeChangeImpactArgs{Service: "checkout", Timestamp: "1700000000000"}, "milliseconds"},
		"event without service": {AnalyzeChangeImpactArgs{ChangeEvent: &change_events.TimeSeries{Values: event.Values}}, "metric.service"},
		"event without a point": {AnalyzeChangeImpactArgs{ChangeEvent: &change_events.TimeSeries{Metric: event.Metric}}, "values"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := &fakeLast9{change: change}
			_, err := runTool(t, f, tc.args)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to mention %q", err, tc.want)
			}
			if len(f.requests) != 0 {
				t.Fatalf("made %d requests before rejecting", len(f.requests))
			}
		})
	}
}

func TestChangeEventFormUsesServiceEnvAndEarliestPoint(t *testing.T) {
	change := recentChange()
	f := &fakeLast9{change: change, baseline: flatWindow(), after: flatWindow()}
	report, err := runTool(t, f, AnalyzeChangeImpactArgs{ChangeEvent: &change_events.TimeSeries{
		Metric: map[string]string{"service": "checkout", "deployment_environment": "prod", "event_name": "deploy"},
		Values: []change_events.TimeSeriesPoint{
			{Timestamp: uint64(change.Add(time.Minute).Unix()), Value: 1},
			{Timestamp: uint64(change.Unix()), Value: 1},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if report.Service != "checkout" || report.Env != "prod" || report.ChangeTime != change.Format(time.RFC3339) {
		t.Fatalf("service=%q env=%q change_time=%q", report.Service, report.Env, report.ChangeTime)
	}
}

func TestUnixSecondsTimestampIsAccepted(t *testing.T) {
	change := recentChange()
	f := &fakeLast9{change: change, baseline: flatWindow(), after: flatWindow()}
	report, err := runTool(t, f, AnalyzeChangeImpactArgs{Service: "checkout", Timestamp: strconv.FormatInt(change.Unix(), 10)})
	if err != nil {
		t.Fatal(err)
	}
	if report.ChangeTime != change.Format(time.RFC3339) {
		t.Fatalf("change_time = %q", report.ChangeTime)
	}
}

func TestWindowMinutesAreClamped(t *testing.T) {
	cases := []struct {
		baseline, after         int
		wantBaseline, wantAfter int
		clamped                 bool
	}{
		{0, 0, 60, 60, false},
		{45, 0, 45, 45, false},
		{10, 0, 30, 30, true},
		{29, 31, 30, 31, true},
		{30, 120, 30, 120, false},
		{121, 500, 120, 120, true},
		{-5, 90, 30, 90, true},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%d/%d", tc.baseline, tc.after), func(t *testing.T) {
			change := time.Now().UTC().Truncate(time.Minute).Add(-3 * time.Hour)
			f := &fakeLast9{change: change, baseline: flatWindow(), after: flatWindow()}
			args := serviceArgs(change)
			args.BaselineMinutes, args.AfterMinutes = tc.baseline, tc.after
			report, err := runTool(t, f, args)
			if err != nil {
				t.Fatal(err)
			}
			w := report.Windows
			if w.BaselineMinutes != tc.wantBaseline || w.AfterMinutes != tc.wantAfter {
				t.Fatalf("windows = %d/%d, want %d/%d", w.BaselineMinutes, w.AfterMinutes, tc.wantBaseline, tc.wantAfter)
			}
			if w.BaselineStart != change.Add(-time.Duration(tc.wantBaseline)*time.Minute).Format(time.RFC3339) ||
				w.AfterEnd != change.Add(time.Duration(tc.wantAfter)*time.Minute).Format(time.RFC3339) {
				t.Fatalf("windows = %+v", w)
			}
			if gotClamped := strings.Contains(strings.Join(report.Notes, " "), "clamped"); gotClamped != tc.clamped {
				t.Fatalf("notes = %v, clamped note expected %v", report.Notes, tc.clamped)
			}
		})
	}
}

func TestAfterWindowIsCappedAtNow(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Minute)
	change := now.Add(-20 * time.Minute)
	f := &fakeLast9{change: change, baseline: flatWindow(), after: flatWindow()}
	report, err := runTool(t, f, serviceArgs(change))
	if err != nil {
		t.Fatal(err)
	}
	if report.Windows.AfterMinutes > 21 || report.Windows.AfterMinutes < 20 || report.Windows.RequestedAfterMinutes != 60 {
		t.Fatalf("windows = %+v, want the after window capped at now", report.Windows)
	}
}
