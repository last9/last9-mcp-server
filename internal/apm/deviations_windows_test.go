package apm

import (
	"testing"
	"time"
)

func TestResolveDeviationWindowsPreviousPeriod(t *testing.T) {
	now := time.Date(2026, 7, 11, 10, 7, 32, 0, time.UTC)
	got, err := resolveDeviationWindows(DeviationArgs{}, now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	if got.TimeSource != "default" || got.BaselineMode != "previous_period" {
		t.Fatalf("unexpected provenance: %+v", got)
	}
	if got.RequestedCurrentEnd.Sub(got.RequestedCurrentStart) != time.Hour {
		t.Fatalf("default window duration = %s, want 1h", got.RequestedCurrentEnd.Sub(got.RequestedCurrentStart))
	}
	if !got.EffectiveCurrentEnd.Before(now) {
		t.Fatalf("incomplete bucket was not excluded: %s", got.EffectiveCurrentEnd)
	}
	if got.EffectiveCurrentEnd != time.Date(2026, 7, 11, 10, 6, 0, 0, time.UTC) {
		t.Fatalf("effective current end = %s", got.EffectiveCurrentEnd)
	}
	if got.EffectiveBaselineEnd != got.EffectiveCurrentStart {
		t.Fatalf("baseline is not immediately before current: %+v", got)
	}
}

func TestResolveDeviationWindowsRelativeLookback(t *testing.T) {
	now := time.Date(2026, 7, 11, 10, 7, 32, 0, time.UTC)
	got, err := resolveDeviationWindows(DeviationArgs{LookbackMinutes: 15}, now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	if got.TimeSource != "relative_lookback" {
		t.Fatalf("time source = %q", got.TimeSource)
	}
	if got.RequestedCurrentStart != now.Add(-15*time.Minute) || got.RequestedCurrentEnd != now {
		t.Fatalf("unexpected requested current window: %+v", got)
	}
	if got.EffectiveCurrentEnd.Sub(got.EffectiveCurrentStart) != 15*time.Minute {
		t.Fatalf("effective duration = %s", got.EffectiveCurrentEnd.Sub(got.EffectiveCurrentStart))
	}
}

func TestResolveDeviationWindowsExplicitCurrentRange(t *testing.T) {
	now := time.Date(2026, 7, 11, 10, 7, 32, 0, time.UTC)
	args := DeviationArgs{
		StartTimeISO: "2026-07-11T07:00:00Z",
		EndTimeISO:   "2026-07-11T08:00:00Z",
	}
	got, err := resolveDeviationWindows(args, now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	if got.TimeSource != "explicit" || got.BaselineMode != "previous_period" {
		t.Fatalf("unexpected provenance: %+v", got)
	}
	if got.EffectiveCurrentStart != time.Date(2026, 7, 11, 7, 0, 0, 0, time.UTC) ||
		got.EffectiveCurrentEnd != time.Date(2026, 7, 11, 8, 0, 0, 0, time.UTC) {
		t.Fatalf("historical completed window changed: %+v", got)
	}
}

func TestResolveDeviationWindowsExplicitBaseline(t *testing.T) {
	now := time.Date(2026, 7, 11, 10, 7, 32, 0, time.UTC)
	args := DeviationArgs{
		StartTimeISO:     "2026-07-11T07:00:00Z",
		EndTimeISO:       "2026-07-11T08:00:00Z",
		BaselineStartISO: "2026-07-10T07:00:00Z",
		BaselineEndISO:   "2026-07-10T08:00:00Z",
	}
	got, err := resolveDeviationWindows(args, now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	if got.BaselineMode != "explicit" {
		t.Fatalf("baseline mode = %q", got.BaselineMode)
	}
	if got.EffectiveBaselineStart != time.Date(2026, 7, 10, 7, 0, 0, 0, time.UTC) ||
		got.EffectiveBaselineEnd != time.Date(2026, 7, 10, 8, 0, 0, 0, time.UTC) {
		t.Fatalf("unexpected effective baseline: %+v", got)
	}
}

func TestResolveDeviationWindowsRejectsPartialPairs(t *testing.T) {
	now := time.Date(2026, 7, 11, 10, 7, 32, 0, time.UTC)
	tests := []DeviationArgs{
		{StartTimeISO: "2026-07-11T07:00:00Z"},
		{EndTimeISO: "2026-07-11T08:00:00Z"},
		{BaselineStartISO: "2026-07-10T07:00:00Z"},
		{BaselineEndISO: "2026-07-10T08:00:00Z"},
	}
	for _, args := range tests {
		if _, err := resolveDeviationWindows(args, now, time.Minute); err == nil {
			t.Fatalf("expected partial pair error for %+v", args)
		}
	}
}

func TestResolveDeviationWindowsRejectsUnequalDurations(t *testing.T) {
	now := time.Date(2026, 7, 11, 10, 7, 32, 0, time.UTC)
	args := DeviationArgs{
		StartTimeISO:     "2026-07-11T07:00:00Z",
		EndTimeISO:       "2026-07-11T08:00:00Z",
		BaselineStartISO: "2026-07-10T07:00:00Z",
		BaselineEndISO:   "2026-07-10T09:00:00Z",
	}
	if _, err := resolveDeviationWindows(args, now, time.Minute); err == nil {
		t.Fatal("expected unequal-duration baseline error")
	}
}

func TestResolveDeviationWindowsAlignsBothWindowsToCompletedBuckets(t *testing.T) {
	now := time.Date(2026, 7, 11, 10, 7, 32, 0, time.UTC)
	got, err := resolveDeviationWindows(DeviationArgs{LookbackMinutes: 10}, now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	if got.EffectiveCurrentStart != time.Date(2026, 7, 11, 9, 56, 0, 0, time.UTC) ||
		got.EffectiveCurrentEnd != time.Date(2026, 7, 11, 10, 6, 0, 0, time.UTC) {
		t.Fatalf("unexpected effective current window: %+v", got)
	}
	if got.EffectiveBaselineStart != time.Date(2026, 7, 11, 9, 46, 0, 0, time.UTC) ||
		got.EffectiveBaselineEnd != time.Date(2026, 7, 11, 9, 56, 0, 0, time.UTC) {
		t.Fatalf("unexpected effective baseline window: %+v", got)
	}
	if got.QueryStep != time.Minute {
		t.Fatalf("query step = %s", got.QueryStep)
	}
}
