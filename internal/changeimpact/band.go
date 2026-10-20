package changeimpact

import (
	"fmt"
	"slices"

	"last9-mcp/internal/apm"
)

// p95 and error rate are judged against the service's own variability: the
// same signal over equal windows before the change. A fixed threshold either
// flagged spiky services' ordinary noise or missed stable services' real rises.
const (
	// The band is this many windows right before the change, each as long as
	// the after window.
	bandWindows = 5
	// With fewer band windows of at least minRequestsPerWindow requests, the
	// fixed thresholds judge instead.
	bandMinWindows = 3
	// An after value above median + this many MADs is past the service's noise.
	bandMADMultiplier = 3.0
	// p95 must also rise at least this much over the band median, so a very
	// stable service does not flag on a tiny move. Error rate must rise
	// errorRateIncreasePP.
	bandP95MinMovePct = 15.0
)

// BaselineBand is one signal over the equal windows before the change that
// carried enough requests to judge it.
type BaselineBand struct {
	Windows       int     `json:"windows"`
	WindowMinutes int     `json:"window_minutes"`
	Min           float64 `json:"min"`
	Max           float64 `json:"max"`
	Median        float64 `json:"median"`
	MAD           float64 `json:"mad"`
}

func newBand(values []float64, windowMinutes int) *BaselineBand {
	if len(values) == 0 {
		return nil
	}
	sorted := slices.Sorted(slices.Values(values))
	m := median(sorted)
	deviations := make([]float64, len(sorted))
	for i, v := range sorted {
		deviations[i] = max(v-m, m-v)
	}
	slices.Sort(deviations)
	return &BaselineBand{
		Windows:       len(values),
		WindowMinutes: windowMinutes,
		Min:           sorted[0],
		Max:           sorted[len(sorted)-1],
		Median:        m,
		MAD:           median(deviations),
	}
}

func median(sorted []float64) float64 {
	mid := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[mid]
	}
	return (sorted[mid-1] + sorted[mid]) / 2
}

func (b *BaselineBand) usable() bool {
	return b != nil && b.Windows >= bandMinWindows
}

func (b *BaselineBand) noiseCeiling() float64 {
	return b.Median + bandMADMultiplier*b.MAD
}

type bandCall int

const (
	withinBand bandCall = iota
	possibleChange
	likelyRegression
)

// judge places after against the band: a regression must clear the range, the
// noise ceiling and minMove over the median; clearing only the range and
// minMove is a possible change.
func (b *BaselineBand) judge(after, minMove float64) bandCall {
	if after <= b.Max || after-b.Median < minMove-thresholdEpsilon {
		return withinBand
	}
	if after > b.noiseCeiling() {
		return likelyRegression
	}
	return possibleChange
}

// bandSignal is one signal judged against its band. format renders a value
// with its unit; where qualifies the value (" in prod").
type bandSignal struct {
	name, format, where string
	band                *BaselineBand
	minMove             float64
	minMoveText         string
}

func (s bandSignal) finding(service string, after float64, call bandCall) string {
	b := s.band
	v := func(x float64) string { return fmt.Sprintf(s.format, x) }
	head := fmt.Sprintf("%s %s %s%s vs baseline range %s–%s over %d windows of %d min", service, s.name, v(after), s.where, v(b.Min), v(b.Max), b.Windows, b.WindowMinutes)
	noise := fmt.Sprintf("median %s + %g×MAD %s = %s", v(b.Median), bandMADMultiplier, v(b.MAD), v(b.noiseCeiling()))
	if call == possibleChange {
		return fmt.Sprintf("%s: above the range but within noise (%s)", head, noise)
	}
	return fmt.Sprintf("%s (threshold: above the range, %s, and %s over the median)", head, noise, s.minMoveText)
}

func errorRateValues(band []apm.GoldenSignals) []float64 {
	var values []float64
	for _, s := range band {
		if s.RequestCount >= minRequestsPerWindow && s.ErrorRatePct != nil {
			values = append(values, *s.ErrorRatePct)
		}
	}
	return values
}

func p95Values(band []apm.GoldenSignals, env string) []float64 {
	var values []float64
	for _, s := range band {
		if p95 := s.LatencyByEnv[env].P95Ms; s.RequestsByEnv[env] >= minRequestsPerWindow && p95 != nil {
			values = append(values, *p95)
		}
	}
	return values
}
