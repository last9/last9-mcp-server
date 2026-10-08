package apm

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestFormatInvestigationBrief_Routing(t *testing.T) {
	for _, tc := range []struct {
		name   string
		json   string
		want   []string
		absent []string
	}{
		{name: "ordered environment associations", json: `{"domains":["apm","rum"],"domain_envs":{"apm":["staging","qa"],"rum":["production"]},"log_indexes":["archive","default"],"log_index_envs":{"archive":["staging"],"default":["production"]}}`, want: []string{"→ domain: apm (staging, qa), rum (production)", "→ log index: archive (staging), default (production)"}},
		{name: "no inferred environments", json: `{"domains":["apm"],"log_indexes":["archive"],"deployment":{"envs":["production"]}}`, want: []string{"→ domain: apm", "→ log index: archive"}, absent: []string{"(production)"}},
		{name: "absent logs retain indexes", json: `{"telemetry":{"logs":"absent"},"log_indexes":["archive"]}`, want: []string{"→ log index: archive"}},
		{name: "missing", json: `{}`, absent: []string{"→ domain:", "→ log index:"}},
		{name: "null", json: `{"domains":null,"domain_envs":null,"log_indexes":null,"log_index_envs":null}`, absent: []string{"→ domain:", "→ log index:"}},
		{name: "empty and orphan mappings", json: `{"domains":[""],"domain_envs":{"apm":["production"]},"log_indexes":[],"log_index_envs":{"archive":["staging"]}}`, absent: []string{"→ domain:", "→ log index:"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var profile serviceProfileResponse
			if err := json.Unmarshal([]byte(tc.json), &profile); err != nil {
				t.Fatal(err)
			}
			got := formatInvestigationBrief(profile)
			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Fatalf("missing %q in %s", want, got)
				}
			}
			for _, absent := range tc.absent {
				if strings.Contains(got, absent) {
					t.Fatalf("unexpected %q in %s", absent, got)
				}
			}
		})
	}
}

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
