package apm

import (
	"fmt"
	"strings"
)

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

func nullCoalesce(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}

func formatInvestigationBrief(p serviceProfileResponse) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Service profile: %s", p.Service)
	if p.DerivedAt != "" {
		fmt.Fprintf(&b, " (derived %s)", p.DerivedAt)
	}
	fmt.Fprintf(&b, "\n  logs: %s | traces: %s | severity_set: %s",
		p.Telemetry.Logs, p.Telemetry.Traces, p.SignalShape.SeveritySet)
	if p.SignalShape.LevelField != "" {
		fmt.Fprintf(&b, " | level_field: %s", p.SignalShape.LevelField)
	}
	if p.SignalShape.SeveritySet == "none" || p.SignalShape.SeveritySet == "partial" {
		fmt.Fprintf(&b, "\n  → parse %s from body; do not use severity_filters", nullCoalesce(p.SignalShape.LevelField, "level"))
	}
	if p.Derivation.LogTier == "failed" {
		fmt.Fprintf(&b, "\n  → log_tier: failed — fall back to get_log_attributes_for_pipeline")
	}
	if p.ErrorDetection != nil && p.ErrorDetection.RecommendedIngestFix != "" {
		fmt.Fprintf(&b, "\n  ingest fix: %s", p.ErrorDetection.RecommendedIngestFix)
	}
	return b.String()
}
