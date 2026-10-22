package apm

import (
	"math"
	"sort"
	"strconv"
)

const (
	// perfSummaryMaxSeries caps the series summarized per metric so a service
	// with many status codes can't grow the default response without bound.
	perfSummaryMaxSeries       = 5
	perfSummaryMaxChangePoints = 3
	// perfSummaryMinSegment is the fewest points each side of a change point
	// must have, so a single outlier is not reported as a level shift.
	perfSummaryMinSegment    = 3
	perfSummaryMinShiftScore = 5.0
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
	Min           float64           `json:"min"`
	P50           float64           `json:"p50"`
	P90           float64           `json:"p90"`
	P99           float64           `json:"p99"`
	Max           float64           `json:"max"`
	Mean          float64           `json:"mean"`
	Last          float64           `json:"last"`
	BaselineMean  float64           `json:"baseline_mean"`
	DeltaPct      *float64          `json:"delta_pct,omitempty"`
	ChangePoints  []PerfChangePoint `json:"change_points,omitempty"`
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
		name   string
		series []TimeSeries
		out    *[]PerfSeriesSummary
		noData func(float64) bool
	}{
		{"throughput", d.Throughput, &s.Throughput, nil},
		{"error_rate", d.ErrorRate, &s.ErrorRate, nil},
		{"error_percentage", d.ErrorPercent, &s.ErrorPercent, nil},
		{"response_times", d.ResponseTimes, &s.ResponseTimes, nil},
		{"apdex_score", d.ApdexScore, &s.ApdexScore, nil},
		{"availability", d.Availability, &s.Availability, func(v float64) bool { return v == perfAvailabilityNoData }},
	}
	for _, f := range fields {
		summaries, omitted := summarizeSeriesList(f.series, f.noData)
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

func summarizeSeriesList(series []TimeSeries, noData func(float64) bool) ([]PerfSeriesSummary, int) {
	out := make([]PerfSeriesSummary, 0, len(series))
	for _, ts := range series {
		out = append(out, summarizeSeries(ts, noData))
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Mean > out[j].Mean })
	if len(out) <= perfSummaryMaxSeries {
		return out, 0
	}
	return out[:perfSummaryMaxSeries], len(out) - perfSummaryMaxSeries
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
	if half > 0 {
		baseline, recent := mean(values[:half]), mean(values[half:])
		s.BaselineMean = roundSig(baseline)
		if baseline != 0 {
			delta := roundSig((recent - baseline) / math.Abs(baseline) * 100)
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

func nearestRank(sorted []float64, p float64) float64 {
	rank := int(math.Ceil(p / 100 * float64(len(sorted))))
	if rank < 1 {
		rank = 1
	}
	return sorted[rank-1]
}

func detectChangePoints(values []float64) []int {
	type split struct {
		idx   int
		score float64
	}
	var found []split
	var segment func(lo, hi int)
	segment = func(lo, hi int) {
		idx, score := bestSplit(values[lo:hi])
		if idx < 0 || score < perfSummaryMinShiftScore {
			return
		}
		found = append(found, split{lo + idx, score})
		segment(lo, lo+idx)
		segment(lo+idx, hi)
	}
	segment(0, len(values))

	sort.SliceStable(found, func(i, j int) bool { return found[i].score > found[j].score })
	if len(found) > perfSummaryMaxChangePoints {
		found = found[:perfSummaryMaxChangePoints]
	}
	idxs := make([]int, len(found))
	for i, f := range found {
		idxs[i] = f.idx
	}
	sort.Ints(idxs)
	return idxs
}

func bestSplit(values []float64) (int, float64) {
	bestIdx, bestScore := -1, 0.0
	for k := perfSummaryMinSegment; k <= len(values)-perfSummaryMinSegment; k++ {
		left, right := values[:k], values[k:]
		ml, mr := mean(left), mean(right)
		shift := math.Abs(mr - ml)
		if shift == 0 || shift < perfSummaryMinRelativeShift*math.Max(math.Abs(ml), math.Abs(mr)) {
			continue
		}
		se := math.Sqrt(variance(left, ml)/float64(len(left)) + variance(right, mr)/float64(len(right)))
		score := math.Inf(1)
		if se > 0 {
			score = shift / se
		}
		if score > bestScore {
			bestIdx, bestScore = k, score
		}
	}
	return bestIdx, bestScore
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
