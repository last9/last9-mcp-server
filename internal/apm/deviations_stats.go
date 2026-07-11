package apm

import (
	"math"
	"sort"
	"time"
)

func summarizeWindow(buckets []bucket, queryStep time.Duration) WindowSummary {
	summary := WindowSummary{ObservedPoints: len(buckets)}
	if len(buckets) == 0 || queryStep <= 0 {
		return summary
	}

	summary.ExpectedPoints = expectedPointCount(buckets, queryStep)
	if summary.ExpectedPoints > 0 {
		summary.Coverage = float64(summary.ObservedPoints) / float64(summary.ExpectedPoints)
	}

	latencies := make([]float64, 0, len(buckets))
	var weightedApdex float64
	for _, point := range buckets {
		summary.RequestTotal += point.Requests
		summary.ErrorTotal += point.Errors
		weightedApdex += point.Apdex * point.Requests
		latencies = append(latencies, point.P95LatencyMS)
	}

	durationMinutes := float64(summary.ExpectedPoints) * queryStep.Minutes()
	if durationMinutes > 0 {
		summary.RequestRPM = summary.RequestTotal / durationMinutes
		summary.ErrorRPM = summary.ErrorTotal / durationMinutes
	}
	if summary.RequestTotal > 0 {
		summary.ErrorPercentage = summary.ErrorTotal / summary.RequestTotal * 100
		summary.Apdex = weightedApdex / summary.RequestTotal
	}
	summary.P95Latency = distribution(latencies)
	summary.Distribution = summary.P95Latency
	return summary
}

func expectedPointCount(buckets []bucket, queryStep time.Duration) int {
	if len(buckets) == 0 {
		return 0
	}
	minTime, maxTime := buckets[0].Timestamp, buckets[0].Timestamp
	if minTime.IsZero() {
		return len(buckets)
	}
	for _, point := range buckets[1:] {
		if point.Timestamp.IsZero() {
			return len(buckets)
		}
		if point.Timestamp.Before(minTime) {
			minTime = point.Timestamp
		}
		if point.Timestamp.After(maxTime) {
			maxTime = point.Timestamp
		}
	}
	return int(maxTime.Sub(minTime)/queryStep) + 1
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
	comparison := SignalComparison{
		Definition:     definition,
		Current:        current,
		Baseline:       baseline,
		AbsoluteDelta:  current.Value - baseline.Value,
		Direction:      valueDirection(current.Value, baseline.Value),
		Classification: "stable",
	}
	if baseline.Value != 0 {
		relative := comparison.AbsoluteDelta / math.Abs(baseline.Value) * 100
		comparison.RelativeDelta = &relative
	}

	comparison.EvidenceQuality = classifyEvidence(current, baseline)
	if current.ObservedPoints == 0 || baseline.ObservedPoints == 0 {
		comparison.Classification = "non_comparable"
		switch {
		case current.ObservedPoints > 0 && baseline.ObservedPoints == 0:
			comparison.PresenceChange = "newly_observed"
		case current.ObservedPoints == 0 && baseline.ObservedPoints > 0:
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

	worsened := upwardCandidate
	if !definition.HigherIsWorse {
		worsened = downwardCandidate
	}
	if worsened {
		comparison.Classification = "regression"
	} else {
		comparison.Classification = "improvement"
	}
	return comparison
}

func classifyEvidence(current, baseline WindowSummary) EvidenceQuality {
	if current.ObservedPoints == 0 || baseline.ObservedPoints == 0 {
		return EvidenceQuality{Level: "non_comparable", Reasons: []string{"series_missing_in_one_or_both_windows"}}
	}
	if current.ObservedPoints < 4 || baseline.ObservedPoints < 4 {
		return EvidenceQuality{Level: "sparse", Reasons: []string{"fewer_than_four_aligned_observations"}}
	}
	if current.ExpectedPoints <= 0 || baseline.ExpectedPoints <= 0 ||
		current.ObservedPoints != current.ExpectedPoints || baseline.ObservedPoints != baseline.ExpectedPoints ||
		current.ExpectedPoints != baseline.ExpectedPoints {
		return EvidenceQuality{Level: "sparse", Reasons: []string{"incomplete_or_unaligned_coverage"}}
	}
	return EvidenceQuality{Level: "sufficient", Reasons: []string{"complete_aligned_coverage"}}
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
