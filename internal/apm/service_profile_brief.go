package apm

import (
	"fmt"
	"strings"
)

// Fields the brief does not render are still decoded so the shape stays
// documented here; the agent reads them from the raw JSON that follows.
type serviceProfileResponse struct {
	Service        string                    `json:"service"`
	DerivedAt      string                    `json:"derived_at"`
	Derivation     derivationStatusResponse  `json:"derivation"`
	Telemetry      telemetryPresenceResponse `json:"telemetry"`
	Deployment     deploymentInfoResponse    `json:"deployment"`
	Language       string                    `json:"language,omitempty"`
	Runtime        *runtimeInfoResponse      `json:"runtime,omitempty"`
	SignalShape    signalShapeResponse       `json:"signal_shape"`
	ErrorDetection *errorDetectionResponse   `json:"error_detection,omitempty"`
	Dependencies   *dependenciesResponse     `json:"dependencies,omitempty"`
	Sources        []string                  `json:"sources,omitempty"`
}

type derivationStatusResponse struct {
	MetricsTier string `json:"metrics_tier"`
	LogTier     string `json:"log_tier"`
}

type telemetryPresenceResponse struct {
	Logs    string `json:"logs"`
	Metrics string `json:"metrics"`
	Traces  string `json:"traces"`
}

type deploymentInfoResponse struct {
	Envs       []string `json:"envs,omitempty"`
	Namespaces []string `json:"namespaces,omitempty"`
}

type runtimeInfoResponse struct {
	Names    []string `json:"names,omitempty"`
	Versions []string `json:"versions,omitempty"`
}

type signalShapeResponse struct {
	LogFormat          string `json:"log_format,omitempty"`
	SeveritySet        string `json:"severity_set"`
	LevelField         string `json:"level_field,omitempty"`
	TraceContextInLogs string `json:"trace_context_in_logs"`
	ParseHint          string `json:"parse_hint,omitempty"`
}

type errorDetectionResponse struct {
	SignaturePack        string `json:"signature_pack,omitempty"`
	ManifestsAs          string `json:"manifests_as,omitempty"`
	RankBy               string `json:"rank_by,omitempty"`
	RecommendedIngestFix string `json:"recommended_ingest_fix,omitempty"`
}

type dependenciesResponse struct {
	DB         []string `json:"db,omitempty"`
	Queue      []string `json:"queue,omitempty"`
	Downstream []string `json:"downstream,omitempty"`
}

func formatInvestigationBrief(p serviceProfileResponse) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Service profile: %s", p.Service)
	if p.DerivedAt != "" {
		fmt.Fprintf(&b, " (derived %s)", p.DerivedAt)
	}

	// Absent tri-state fields render as "unknown" rather than a blank gap the
	// model would have to guess at.
	logs := firstNonEmpty(p.Telemetry.Logs, "unknown")
	traces := firstNonEmpty(p.Telemetry.Traces, "unknown")
	severity := firstNonEmpty(p.SignalShape.SeveritySet, "unknown")

	fmt.Fprintf(&b, "\n  logs: %s | traces: %s | severity_set: %s", logs, traces, severity)
	if p.SignalShape.LevelField != "" {
		fmt.Fprintf(&b, " | level_field: %s", p.SignalShape.LevelField)
	}

	// Severity routing is advice about querying logs; with no logs to query it
	// is noise competing with the name-check hint below.
	if logs != "absent" {
		switch severity {
		case "none", "partial":
			if p.SignalShape.LevelField != "" {
				fmt.Fprintf(&b, "\n  → parse %s from body; do not use severity_filters", p.SignalShape.LevelField)
			} else {
				// Naming a field the profile did not report would be a guess the
				// model then queries on.
				fmt.Fprintf(&b, "\n  → parse the level from the log body; do not use severity_filters")
			}
		case "unknown":
			// Silence here would leave severity_filters looking safe on a service
			// where nobody has established that it works.
			fmt.Fprintf(&b, "\n  → severity coverage undetermined; verify before relying on severity_filters")
		}
	}

	if p.Derivation.LogTier == "failed" {
		fmt.Fprintf(&b, "\n  → log_tier: failed — fall back to get_log_attributes_for_pipeline")
	}
	// A typo'd service name derives the same empty profile as a real unmonitored
	// service; only the name check separates them.
	if logs == "absent" && traces == "absent" {
		fmt.Fprintf(&b, "\n  → no telemetry under this exact name; confirm the spelling with did_you_mean before concluding the service is unmonitored")
	}
	if p.ErrorDetection != nil && p.ErrorDetection.RecommendedIngestFix != "" {
		fmt.Fprintf(&b, "\n  ingest fix: %s", p.ErrorDetection.RecommendedIngestFix)
	}
	return b.String()
}
