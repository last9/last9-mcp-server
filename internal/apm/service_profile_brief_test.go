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
		Service:     "api",
		SignalShape: signalShapeResponse{SeveritySet: "partial", LevelField: "level"},
	}
	got := formatInvestigationBrief(p)
	if !strings.Contains(got, "do not use severity_filters") {
		t.Fatalf("partial should route like none:\n%s", got)
	}
}

// The routing hint firing on a service with usable severity would send every
// investigation down the body-parse path; presence-only assertions miss it.
func TestFormatInvestigationBrief_SeverityFullOmitsRoutingHint(t *testing.T) {
	p := serviceProfileResponse{
		Service:     "api",
		SignalShape: signalShapeResponse{SeveritySet: "full"},
		Telemetry:   telemetryPresenceResponse{Logs: "present", Traces: "present"},
	}
	got := formatInvestigationBrief(p)
	for _, unwanted := range []string{
		"do not use severity_filters",
		"severity coverage undetermined",
	} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("severity_set full must not emit %q:\n%s", unwanted, got)
		}
	}
}

func TestFormatInvestigationBrief_SeverityUnknownWarns(t *testing.T) {
	p := serviceProfileResponse{
		Service:   "api",
		Telemetry: telemetryPresenceResponse{Logs: "present", Traces: "present"},
	}
	got := formatInvestigationBrief(p)
	if !strings.Contains(got, "severity coverage undetermined") {
		t.Fatalf("undetermined severity must warn rather than stay silent:\n%s", got)
	}
}

// Missing tri-state fields used to render "logs:  | traces:  | severity_set: ".
func TestFormatInvestigationBrief_MissingFieldsRenderUnknown(t *testing.T) {
	got := formatInvestigationBrief(serviceProfileResponse{Service: "api"})
	want := "logs: unknown | traces: unknown | severity_set: unknown"
	if !strings.Contains(got, want) {
		t.Fatalf("want %q in:\n%s", want, got)
	}
}

// Severity advice is about querying logs; with none to query it competes with
// the name-check hint.
func TestFormatInvestigationBrief_NoSeverityAdviceWhenLogsAbsent(t *testing.T) {
	p := serviceProfileResponse{
		Service:   "api",
		Telemetry: telemetryPresenceResponse{Logs: "absent", Traces: "present"},
	}
	got := formatInvestigationBrief(p)
	if strings.Contains(got, "severity coverage undetermined") {
		t.Fatalf("no logs means no severity advice:\n%s", got)
	}
}

// Naming a field the profile never reported is a guess the model then queries on.
func TestFormatInvestigationBrief_NoLevelFieldStaysGeneric(t *testing.T) {
	p := serviceProfileResponse{
		Service:     "api",
		Telemetry:   telemetryPresenceResponse{Logs: "present"},
		SignalShape: signalShapeResponse{SeveritySet: "none"},
	}
	got := formatInvestigationBrief(p)
	if strings.Contains(got, "parse level from body") {
		t.Fatalf("must not invent a level field name:\n%s", got)
	}
	if !strings.Contains(got, "parse the level from the log body") {
		t.Fatalf("want generic body-parse guidance:\n%s", got)
	}
}

func TestFormatInvestigationBrief_NoTelemetrySuggestsNameCheck(t *testing.T) {
	p := serviceProfileResponse{
		Service:    "typoed-svc",
		Telemetry:  telemetryPresenceResponse{Logs: "absent", Traces: "absent"},
		Derivation: derivationStatusResponse{LogTier: "skipped"},
	}
	got := formatInvestigationBrief(p)
	if !strings.Contains(got, "did_you_mean") {
		t.Fatalf("empty profile should suggest a name check:\n%s", got)
	}
}

func TestFormatInvestigationBrief_LogTierFailed(t *testing.T) {
	p := serviceProfileResponse{
		Service:    "api",
		Derivation: derivationStatusResponse{LogTier: "failed"},
	}
	got := formatInvestigationBrief(p)
	if !strings.Contains(got, "log_tier: failed") {
		t.Fatalf("expected failed tier warning:\n%s", got)
	}
}
