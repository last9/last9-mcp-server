package apm

import (
	"strings"
	"testing"
)

func TestFormatInvestigationBrief_SeverityNone(t *testing.T) {
	p := serviceProfileResponse{
		Service:   "payment-service",
		DerivedAt: "2026-08-05T12:00:00Z",
		SignalShape: signalShapeResponse{
			SeveritySet: "none",
			LevelField:  "level",
		},
		Telemetry: telemetryPresenceResponse{Logs: "present", Traces: "present"},
		ErrorDetection: &errorDetectionResponse{
			RecommendedIngestFix: `promote body field "level" to SeverityText`,
		},
	}
	got := formatInvestigationBrief(p)
	for _, want := range []string{
		"payment-service",
		"severity_set: none",
		"level_field: level",
		"do not use severity_filters",
		"parse",
		"ingest fix:",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("brief missing %q:\n%s", want, got)
		}
	}
}

func TestFormatInvestigationBrief_SeverityPartial(t *testing.T) {
	p := serviceProfileResponse{
		Service: "api",
		SignalShape: signalShapeResponse{SeveritySet: "partial", LevelField: "level"},
	}
	got := formatInvestigationBrief(p)
	if !strings.Contains(got, "do not use severity_filters") {
		t.Fatalf("partial should route like none:\n%s", got)
	}
}

func TestFormatInvestigationBrief_LogTierFailed(t *testing.T) {
	p := serviceProfileResponse{
		Service: "api",
		Derivation: derivationStatusResponse{LogTier: "failed"},
	}
	got := formatInvestigationBrief(p)
	if !strings.Contains(got, "log_tier: failed") {
		t.Fatalf("expected failed tier warning:\n%s", got)
	}
}
