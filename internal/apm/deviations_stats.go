package apm

import (
	"math"
	"sort"
)

func newWindowEvidence(expectedPoints int) WindowEvidence {
	evidence := MetricEvidence{ExpectedPoints: expectedPoints}
	return WindowEvidence{
		Requests:        evidence,
		Errors:          evidence,
		ErrorPercentage: evidence,
		Apdex:           evidence,
		P95Latency:      evidence,
	}
}

func withCoverage(evidence WindowEvidence) WindowEvidence {
	evidence.Selected = calculateCoverage(evidence.Selected)
	evidence.Requests = calculateCoverage(evidence.Requests)
	evidence.Errors = calculateCoverage(evidence.Errors)
	evidence.ErrorPercentage = calculateCoverage(evidence.ErrorPercentage)
	evidence.Apdex = calculateCoverage(evidence.Apdex)
	evidence.P95Latency = calculateCoverage(evidence.P95Latency)
	return evidence
}

func calculateCoverage(evidence MetricEvidence) MetricEvidence {
	if evidence.ExpectedPoints > 0 {
		evidence.Coverage = math.Min(1, float64(evidence.ObservedPoints)/float64(evidence.ExpectedPoints))
	}
	evidence.MissingValues = max(evidence.ExpectedPoints-evidence.ObservedPoints-evidence.ExcludedValues, 0)
	return evidence
}

func isFinite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func distribution(values []float64) Distribution {
	if len(values) == 0 {
		return Distribution{}
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	q25 := quantile(sorted, 0.25)
	q75 := quantile(sorted, 0.75)
	return Distribution{
		Q25:    q25,
		Median: quantile(sorted, 0.5),
		Q75:    q75,
		IQR:    q75 - q25,
		Peak:   sorted[len(sorted)-1],
	}
}

func quantile(sorted []float64, q float64) float64 {
	if len(sorted) == 1 {
		return sorted[0]
	}
	position := q * float64(len(sorted)-1)
	lower := int(math.Floor(position))
	upper := int(math.Ceil(position))
	if lower == upper {
		return sorted[lower]
	}
	weight := position - float64(lower)
	return sorted[lower]*(1-weight) + sorted[upper]*weight
}

func compareSignal(definition SignalDefinition, current, baseline WindowSummary) SignalComparison {
	current = finiteWindowSummary(current)
	baseline = finiteWindowSummary(baseline)
	comparison := SignalComparison{
		Definition:     definition,
		Current:        current,
		Baseline:       baseline,
		AbsoluteDelta:  current.Value - baseline.Value,
		Direction:      valueDirection(current.Value, baseline.Value),
		Classification: "stable",
	}
	if baseline.Value != 0 {
		relative := comparison.AbsoluteDelta / math.Abs(baseline.Value)
		comparison.RelativeDelta = &relative
	}

	currentEvidence := selectedEvidence(definition.Name, current.Evidence)
	baselineEvidence := selectedEvidence(definition.Name, baseline.Evidence)
	comparison.EvidenceQuality = classifyEvidence(currentEvidence, baselineEvidence)
	if currentEvidence.ObservedPoints == 0 || baselineEvidence.ObservedPoints == 0 {
		comparison.Classification = "non_comparable"
		switch {
		case currentEvidence.ObservedPoints > 0 && baselineEvidence.ObservedPoints == 0:
			comparison.PresenceChange = "newly_observed"
		case currentEvidence.ObservedPoints == 0 && baselineEvidence.ObservedPoints > 0:
			comparison.PresenceChange = "no_longer_observed"
		}
		return comparison
	}
	if comparison.EvidenceQuality.Level != "sufficient" {
		comparison.Classification = "insufficient_evidence"
		return comparison
	}

	upwardCandidate := current.Distribution.Median > baseline.Distribution.Q75 &&
		baseline.Distribution.Median < current.Distribution.Q25
	downwardCandidate := current.Distribution.Median < baseline.Distribution.Q25 &&
		baseline.Distribution.Median > current.Distribution.Q75
	if !upwardCandidate && !downwardCandidate {
		return comparison
	}
	primaryUpward := comparison.AbsoluteDelta > 0
	primaryDownward := comparison.AbsoluteDelta < 0
	if (upwardCandidate && !primaryUpward) || (downwardCandidate && !primaryDownward) {
		return comparison
	}

	worsened := primaryUpward
	if !definition.HigherIsWorse {
		worsened = primaryDownward
	}
	if worsened {
		comparison.Classification = "regression"
	} else {
		comparison.Classification = "improvement"
	}
	return comparison
}

func selectedEvidence(signalName string, evidence WindowEvidence) MetricEvidence {
	if evidence.Selected.ExpectedPoints != 0 || evidence.Selected.ObservedPoints != 0 ||
		evidence.Selected.MissingValues != 0 || evidence.Selected.ExcludedValues != 0 {
		return evidence.Selected
	}
	switch signalName {
	case "request_rpm", "requests", "throughput":
		return evidence.Requests
	case "error_throughput_rpm", "error_rpm", "errors":
		return evidence.Errors
	case "error_percentage":
		return evidence.ErrorPercentage
	case "apdex":
		return evidence.Apdex
	case "p95_latency", "p95_latency_ms":
		return evidence.P95Latency
	default:
		return evidence.Selected
	}
}

func classifyEvidence(current, baseline MetricEvidence) EvidenceQuality {
	if current.ObservedPoints == 0 || baseline.ObservedPoints == 0 {
		return EvidenceQuality{Level: "non_comparable", Reasons: []string{"series_missing_in_one_or_both_windows"}}
	}
	if current.ObservedPoints < 4 || baseline.ObservedPoints < 4 {
		return EvidenceQuality{Level: "sparse", Reasons: []string{"fewer_than_four_aligned_observations"}}
	}
	if current.ExpectedPoints <= 0 || baseline.ExpectedPoints <= 0 ||
		current.ObservedPoints != current.ExpectedPoints || baseline.ObservedPoints != baseline.ExpectedPoints ||
		current.ExpectedPoints != baseline.ExpectedPoints || current.ExcludedValues > 0 || baseline.ExcludedValues > 0 {
		return EvidenceQuality{Level: "sparse", Reasons: []string{"incomplete_or_unaligned_coverage"}}
	}
	return EvidenceQuality{Level: "sufficient", Reasons: []string{"complete_aligned_coverage"}}
}

func finiteWindowSummary(summary WindowSummary) WindowSummary {
	if !isFinite(summary.Value) {
		summary.Value = 0
		summary.Evidence.Selected.ExcludedValues++
		summary.Evidence.Selected.ObservedPoints = 0
	}
	if !isFinite(summary.RequestTotal) {
		summary.RequestTotal = 0
	}
	if !isFinite(summary.ErrorTotal) {
		summary.ErrorTotal = 0
	}
	if !isFinite(summary.RequestRPM) {
		summary.RequestRPM = 0
	}
	if !isFinite(summary.ErrorRPM) {
		summary.ErrorRPM = 0
	}
	if summary.ErrorPercentage != nil && !isFinite(*summary.ErrorPercentage) {
		summary.ErrorPercentage = nil
	}
	if summary.Apdex != nil && !isFinite(*summary.Apdex) {
		summary.Apdex = nil
	}
	if summary.P95Latency != nil && !finiteDistribution(*summary.P95Latency) {
		summary.P95Latency = nil
	}
	if !finiteDistribution(summary.Distribution) {
		summary.Distribution = Distribution{}
		summary.Evidence.Selected.ExcludedValues++
		summary.Evidence.Selected.ObservedPoints = 0
	}
	return summary
}

func finiteDistribution(value Distribution) bool {
	return isFinite(value.Q25) && isFinite(value.Median) && isFinite(value.Q75) && isFinite(value.IQR) && isFinite(value.Peak)
}

func valueDirection(current, baseline float64) string {
	switch {
	case current > baseline:
		return "increased"
	case current < baseline:
		return "decreased"
	default:
		return "unchanged"
	}
}
