package apm

import (
	"math"
	"sort"
	"strconv"
)

const (
	// perfSummaryMaxSeries caps the series summarized per metric so a service
	// with many status codes can't grow the default response without bound.
	// Response times are exempt: one series per quantile, a fixed set.
	perfSummaryMaxSeries       = 4
	perfSummaryMaxChangePoints = 3
	// perfSummaryMinSegment is the fewest points each side of a change point,
	// and each half of a baseline delta, must have, so a single outlier is not
	// reported as a level shift.
	perfSummaryMinSegment = 3
	// perfSummaryMinSegmentDivisor raises the minimum segment to a tenth of
	// the series: a few correlated points at an edge are noise, not a level.
	perfSummaryMinSegmentDivisor = 10
	perfSummaryMinShiftScore     = 5.0
	// perfSummaryMinRelativeShift ignores statistically sharp but negligible
	// shifts (a flat series with a tiny wobble scores a huge t).
	perfSummaryMinRelativeShift = 0.1
	// perfSummarySigDigits rounds summary numbers to keep the payload small.
	perfSummarySigDigits = 4
	// perfSummaryDefaultTopN replaces perfDetailsDefaultTopN when top_n is
	// unset in summary mode: the top-k lists dominate the summary's size.
	perfSummaryDefaultTopN    = 5
	perfSummaryFullSeriesNote = "omitted; call again with include_series=true for every point of every series"
)

type ServicePerformanceSummary struct {
	ServiceName   string              `json:"service_name"`
	Env           string              `json:"env"`
	EnvRequested  string              `json:"env_requested,omitempty"`
	FullSeries    string              `json:"full_series"`
	Throughput    []PerfSeriesSummary `json:"throughput"`
	ErrorRate     []PerfSeriesSummary `json:"error_rate"`
	ErrorPercent  []PerfSeriesSummary `json:"error_percentage"`
	ResponseTimes []PerfSeriesSummary `json:"response_times"`
	ApdexScore    []PerfSeriesSummary `json:"apdex_score"`
	Availability  []PerfSeriesSummary `json:"availability"`
	SeriesOmitted map[string]int      `json:"series_omitted,omitempty"`
	TopOperations perfTopOperations   `json:"top_operations"`
	TopErrors     []map[string]int64  `json:"top_errors"`
	PartialErrors []string            `json:"partial_errors,omitempty"`
}

type perfTopOperations struct {
	ByResponseTime []map[string]float64 `json:"by_response_time"`
	ByErrorRate    []map[string]int64   `json:"by_error_rate"`
}

type PerfSeriesSummary struct {
	Metric        map[string]string `json:"metric,omitempty"`
	Points        int               `json:"points"`
	MissingPoints int               `json:"missing_points,omitempty"`
	// Nil when Points is 0, so a series without data shows no zeros.
	*PerfSeriesStats
}

type PerfSeriesStats struct {
	Min          float64           `json:"min"`
	P50          float64           `json:"p50"`
	P90          float64           `json:"p90"`
	P99          float64           `json:"p99"`
	Max          float64           `json:"max"`
	Mean         float64           `json:"mean"`
	Last         float64           `json:"last"`
	BaselineMean *float64          `json:"baseline_mean,omitempty"`
	DeltaPct     *float64          `json:"delta_pct,omitempty"`
	ChangePoints []PerfChangePoint `json:"change_points,omitempty"`
}

type PerfChangePoint struct {
	Timestamp  uint64  `json:"timestamp"`
	BeforeMean float64 `json:"before_mean"`
	AfterMean  float64 `json:"after_mean"`
}

func summarizePerformanceDetails(d ServicePerformanceDetails) ServicePerformanceSummary {
	s := ServicePerformanceSummary{
		ServiceName:   d.ServiceName,
		Env:           d.Env,
		EnvRequested:  d.EnvRequested,
		FullSeries:    perfSummaryFullSeriesNote,
		TopOperations: perfTopOperations{ByResponseTime: d.TopOperations.ByResponseTime, ByErrorRate: d.TopOperations.ByErrorRate},
		TopErrors:     d.TopErrors,
		PartialErrors: d.PartialErrors,
	}
	fields := []struct {
		name      string
		series    []TimeSeries
		out       *[]PerfSeriesSummary
		noData    func(float64) bool
		maxSeries int
	}{
		{"throughput", d.Throughput, &s.Throughput, nil, perfSummaryMaxSeries},
		{"error_rate", d.ErrorRate, &s.ErrorRate, nil, perfSummaryMaxSeries},
		{"error_percentage", d.ErrorPercent, &s.ErrorPercent, nil, perfSummaryMaxSeries},
		{"response_times", d.ResponseTimes, &s.ResponseTimes, nil, len(d.ResponseTimes)},
		{"apdex_score", d.ApdexScore, &s.ApdexScore, nil, perfSummaryMaxSeries},
		{"availability", d.Availability, &s.Availability, func(v float64) bool { return v == perfAvailabilityNoData }, perfSummaryMaxSeries},
	}
	for _, f := range fields {
		summaries, omitted := summarizeSeriesList(f.series, f.noData, f.maxSeries)
		*f.out = summaries
		if omitted > 0 {
			if s.SeriesOmitted == nil {
				s.SeriesOmitted = map[string]int{}
			}
			s.SeriesOmitted[f.name] = omitted
		}
	}
	return s
}

func summarizeSeriesList(series []TimeSeries, noData func(float64) bool, maxSeries int) ([]PerfSeriesSummary, int) {
	out := make([]PerfSeriesSummary, 0, len(series))
	for _, ts := range series {
		out = append(out, summarizeSeries(ts, noData))
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].rankMean() > out[j].rankMean() })
	if len(out) <= maxSeries {
		return out, 0
	}
	return out[:maxSeries], len(out) - maxSeries
}

func summarizeSeries(ts TimeSeries, noData func(float64) bool) PerfSeriesSummary {
	var stamps []uint64
	var values []float64
	missing := 0
	for _, p := range ts.Values {
		if math.IsNaN(p.Value) || math.IsInf(p.Value, 0) {
			continue
		}
		if noData != nil && noData(p.Value) {
			missing++
			continue
		}
		stamps = append(stamps, p.Timestamp)
		values = append(values, p.Value)
	}
	s := PerfSeriesSummary{Metric: ts.Metric, Points: len(values), MissingPoints: missing}
	if len(values) == 0 {
		return s
	}
	s.PerfSeriesStats = &PerfSeriesStats{}

	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	half := len(values) / 2
	s.Min = roundSig(sorted[0])
	s.P50 = roundSig(nearestRank(sorted, 50))
	s.P90 = roundSig(nearestRank(sorted, 90))
	s.P99 = roundSig(nearestRank(sorted, 99))
	s.Max = roundSig(sorted[len(sorted)-1])
	s.Mean = roundSig(mean(values))
	s.Last = roundSig(values[len(values)-1])
	if half >= perfSummaryMinSegment {
		baseline, recent := mean(values[:half]), mean(values[half:])
		baselineMean := roundSig(baseline)
		s.BaselineMean = &baselineMean
		if delta := (recent - baseline) / math.Abs(baseline) * 100; baseline != 0 && !math.IsInf(delta, 0) {
			delta = roundSig(delta)
			s.DeltaPct = &delta
		}
	}
	cps := detectChangePoints(values)
	bounds := append(append([]int{0}, cps...), len(values))
	for i, idx := range cps {
		s.ChangePoints = append(s.ChangePoints, PerfChangePoint{
			Timestamp:  stamps[idx],
			BeforeMean: roundSig(mean(values[bounds[i]:idx])),
			AfterMean:  roundSig(mean(values[idx:bounds[i+2]])),
		})
	}
	return s
}

// rankMean orders series without data after every series with data.
func (s PerfSeriesSummary) rankMean() float64 {
	if s.PerfSeriesStats == nil {
		return math.Inf(-1)
	}
	return s.Mean
}

func nearestRank(sorted []float64, p float64) float64 {
	rank := int(math.Ceil(p / 100 * float64(len(sorted))))
	if rank < 1 {
		rank = 1
	}
	return sorted[rank-1]
}

type perfSplit struct {
	idx   int
	score float64
}

func detectChangePoints(values []float64) []int {
	// Segmenting once as if points were independent removes real level
	// shifts, so the residual correlation left is the noise's own.
	inflation := varianceInflation(values, splitIndexes(segmentSplits(values, 1)))
	return dropNegligibleShifts(values, splitIndexes(strongestSplits(segmentSplits(values, inflation))))
}

func strongestSplits(found []perfSplit) []perfSplit {
	sort.SliceStable(found, func(i, j int) bool { return found[i].score > found[j].score })
	if len(found) > perfSummaryMaxChangePoints {
		found = found[:perfSummaryMaxChangePoints]
	}
	return found
}

// segmentSplits is binary segmentation: split where the shift scores
// highest, then recurse into both sides.
func segmentSplits(values []float64, inflation float64) []perfSplit {
	minSegment := max(perfSummaryMinSegment, len(values)/perfSummaryMinSegmentDivisor)
	var found []perfSplit
	var segment func(lo, hi int)
	segment = func(lo, hi int) {
		idx, score := bestSplit(values[lo:hi], inflation, minSegment)
		if idx < 0 || score < perfSummaryMinShiftScore {
			return
		}
		found = append(found, perfSplit{lo + idx, score})
		segment(lo, lo+idx)
		segment(lo+idx, hi)
	}
	segment(0, len(values))
	return found
}

func splitIndexes(splits []perfSplit) []int {
	idxs := make([]int, len(splits))
	for i, s := range splits {
		idxs[i] = s.idx
	}
	sort.Ints(idxs)
	return idxs
}

// dropNegligibleShifts re-applies the relative-shift gate to the means each
// change point reports, which span to its neighbours rather than the split
// the gate tested, removing the smallest failing shift until all pass.
func dropNegligibleShifts(values []float64, idxs []int) []int {
	for {
		weakest, weakestShift := -1, perfSummaryMinRelativeShift
		for i, idx := range idxs {
			lo, hi := 0, len(values)
			if i > 0 {
				lo = idxs[i-1]
			}
			if i+1 < len(idxs) {
				hi = idxs[i+1]
			}
			if shift := relativeShift(mean(values[lo:idx]), mean(values[idx:hi])); shift < weakestShift {
				weakest, weakestShift = i, shift
			}
		}
		if weakest < 0 {
			return idxs
		}
		idxs = append(idxs[:weakest], idxs[weakest+1:]...)
	}
}

func relativeShift(before, after float64) float64 {
	shift := math.Abs(after - before)
	if shift == 0 {
		return 0
	}
	return shift / math.Max(math.Abs(before), math.Abs(after))
}

func bestSplit(values []float64, inflation float64, minSegment int) (int, float64) {
	bestIdx, bestScore := -1, 0.0
	for k := minSegment; k <= len(values)-minSegment; k++ {
		left, right := values[:k], values[k:]
		ml, mr := mean(left), mean(right)
		if relativeShift(ml, mr) < perfSummaryMinRelativeShift {
			continue
		}
		se := math.Sqrt(inflation * (variance(left, ml)/float64(len(left)) + variance(right, mr)/float64(len(right))))
		score := math.Inf(1)
		if se > 0 {
			score = math.Abs(mr-ml) / se
		}
		if score > bestScore {
			bestIdx, bestScore = k, score
		}
	}
	return bestIdx, bestScore
}

// varianceInflation is (1+ρ)/(1-ρ) for the lag-1 autocorrelation ρ of
// residuals around each segment's mean: neighbouring points are correlated
// (rates are smoothed over several steps), so a series holds fewer
// independent samples than points.
func varianceInflation(values []float64, splits []int) float64 {
	bounds := append(append([]int{0}, splits...), len(values))
	var num, den float64
	for i := 0; i+1 < len(bounds); i++ {
		seg := values[bounds[i]:bounds[i+1]]
		m := mean(seg)
		for j, v := range seg {
			den += (v - m) * (v - m)
			if j > 0 {
				num += (v - m) * (seg[j-1] - m)
			}
		}
	}
	if den == 0 {
		return 1
	}
	// Residuals around a short segment's own mean understate ρ by about
	// (1+3ρ)/length (Marriott and Pope, 1954).
	rho := num / den
	rho += (1 + 3*rho) / (float64(len(values)) / float64(len(bounds)-1))
	rho = math.Min(math.Max(rho, 0), 0.99)
	return (1 + rho) / (1 - rho)
}

func mean(values []float64) float64 {
	var sum float64
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

func variance(values []float64, m float64) float64 {
	var sum float64
	for _, v := range values {
		sum += (v - m) * (v - m)
	}
	return sum / float64(len(values))
}

func roundSig(v float64) float64 {
	r, _ := strconv.ParseFloat(strconv.FormatFloat(v, 'g', perfSummarySigDigits, 64), 64)
	return r
}
