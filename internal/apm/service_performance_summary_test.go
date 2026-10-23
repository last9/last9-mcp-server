package apm

import (
	"encoding/json"
	"math"
	"math/rand"
	"reflect"
	"testing"
)

func pointsFrom(values ...float64) []TimeSeriesPoint {
	pts := make([]TimeSeriesPoint, len(values))
	for i, v := range values {
		pts[i] = TimeSeriesPoint{Timestamp: uint64(1000 + 60*i), Value: v}
	}
	return pts
}

func TestSummarizeSeries_NearestRankPercentiles(t *testing.T) {
	values := make([]float64, 100)
	for i := range values {
		values[i] = float64(100 - i) // descending, so the summary must sort
	}
	s := summarizeSeries(TimeSeries{Metric: map[string]string{"quantile": "p95"}, Values: pointsFrom(values...)}, nil)

	if s.Points != 100 || s.Min != 1 || s.P50 != 50 || s.P90 != 90 || s.P99 != 99 || s.Max != 100 {
		t.Fatalf("points/min/p50/p90/p99/max = %d/%v/%v/%v/%v/%v, want 100/1/50/90/99/100", s.Points, s.Min, s.P50, s.P90, s.P99, s.Max)
	}
	if s.Mean != 50.5 || s.Last != 1 {
		t.Fatalf("mean/last = %v/%v, want 50.5/1", s.Mean, s.Last)
	}
	if s.BaselineMean == nil || *s.BaselineMean != 75.5 || s.DeltaPct == nil || *s.DeltaPct != -66.23 {
		t.Fatalf("baseline/delta = %v/%v, want 75.5/-66.23", s.BaselineMean, s.DeltaPct)
	}
}

func TestSummarizeSeries_SinglePoint(t *testing.T) {
	s := summarizeSeries(TimeSeries{Values: pointsFrom(7)}, nil)
	if s.Points != 1 || s.P50 != 7 || s.P99 != 7 || s.Last != 7 {
		t.Fatalf("unexpected single-point summary: %+v", s)
	}
	if s.DeltaPct != nil || s.ChangePoints != nil {
		t.Fatalf("single point must carry no delta or change points: %+v", s)
	}
}

func TestSummarizeSeries_SkipsNonFiniteAndEmpty(t *testing.T) {
	s := summarizeSeries(TimeSeries{Values: pointsFrom(math.NaN(), math.Inf(1), 4, math.Inf(-1))}, nil)
	if s.Points != 1 || s.Max != 4 {
		t.Fatalf("non-finite points must be skipped: %+v", s)
	}
	if empty := summarizeSeries(TimeSeries{}, nil); empty.Points != 0 || empty.PerfSeriesStats != nil {
		t.Fatalf("empty series: %+v", empty)
	}
}

// The baseline is the first half of the valid points, not of the window: six
// points clustered at the end of an hour still split three and three.
func TestSummarizeSeries_BaselineSplitsValidPointsNotWindow(t *testing.T) {
	var pts []TimeSeriesPoint
	for i, v := range []float64{10, 10, 10, 20, 20, 20} {
		pts = append(pts, TimeSeriesPoint{Timestamp: uint64(3000 + 60*i), Value: v})
	}
	s := summarizeSeries(TimeSeries{Values: pts}, nil)
	if s.BaselineMean == nil || *s.BaselineMean != 10 || s.DeltaPct == nil || *s.DeltaPct != 100 {
		t.Fatalf("baseline/delta = %v/%v, want 10/100", s.BaselineMean, s.DeltaPct)
	}
}

// A series with no valid points must not report zeros an agent reads as
// "throughput 0"; one point has no first half, so no baseline either.
func TestSummarizeSeries_UndefinedStatsAreOmitted(t *testing.T) {
	cases := []struct {
		name   string
		series TimeSeries
		want   string
	}{
		{"empty", TimeSeries{}, `{"points":0}`},
		{"only no-data", TimeSeries{Values: pointsFrom(-999, -999)}, `{"points":0,"missing_points":2}`},
		{"single point", TimeSeries{Values: pointsFrom(7)}, `{"points":1,"min":7,"p50":7,"p90":7,"p99":7,"max":7,"mean":7,"last":7}`},
		{"zero value kept", TimeSeries{Values: pointsFrom(0, 0)}, `{"points":2,"min":0,"p50":0,"p90":0,"p99":0,"max":0,"mean":0,"last":0,"baseline_mean":0}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := json.Marshal(summarizeSeries(c.series, func(v float64) bool { return v == perfAvailabilityNoData }))
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != c.want {
				t.Fatalf("got %s, want %s", got, c.want)
			}
		})
	}
}

// A baseline so close to zero that the ratio overflows must drop delta_pct,
// not fail the whole response with json: unsupported value: +Inf.
func TestSummarizeSeries_OverflowingDeltaIsOmitted(t *testing.T) {
	s := summarizeSeries(TimeSeries{Values: pointsFrom(1e-310, 1e-310, 1, 1)}, nil)
	if s.DeltaPct != nil {
		t.Fatalf("delta_pct = %v, want omitted", *s.DeltaPct)
	}
	if _, err := json.Marshal(s); err != nil {
		t.Fatalf("summary must marshal: %v", err)
	}
}

func TestSummarizeSeries_ZeroBaselineHasNoDelta(t *testing.T) {
	s := summarizeSeries(TimeSeries{Values: pointsFrom(0, 0, 5, 5)}, nil)
	if s.DeltaPct != nil {
		t.Fatalf("delta_pct against a zero baseline must be omitted, got %v", *s.DeltaPct)
	}
}

func TestDetectChangePoints(t *testing.T) {
	step := func(levels ...float64) []float64 {
		var out []float64
		for _, l := range levels {
			for i := 0; i < 20; i++ {
				out = append(out, l+float64(i%3)*0.1)
			}
		}
		return out
	}
	cases := []struct {
		name   string
		values []float64
		want   []int
	}{
		{"flat", step(10), []int{}},
		{"one shift", step(10, 50), []int{20}},
		{"two shifts", step(10, 50, 10), []int{20, 40}},
		{"capped at the strongest three", step(10, 100, 10, 100, 10), nil},
		{"negligible relative shift", append(step(1000), step(1001)...), []int{}},
		{"lone outlier", append(append(step(10), 500), step(10)...), []int{}},
		{"shorter than two segments", []float64{1, 2, 3, 100, 100}, []int{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := detectChangePoints(c.values)
			if c.want == nil {
				if len(got) != perfSummaryMaxChangePoints {
					t.Fatalf("got %v, want %d change points", got, perfSummaryMaxChangePoints)
				}
				return
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("got %v, want %v", got, c.want)
			}
		})
	}
}

// ar1Noise is a level with autocorrelated noise, like per-minute rates
// smoothed over a multi-minute rate window: no level shift anywhere.
func ar1Noise(seed int64, n int, level, sd, rho float64) []float64 {
	r := rand.New(rand.NewSource(seed))
	out := make([]float64, n)
	e := 0.0
	for i := range out {
		e = rho*e + math.Sqrt(1-rho*rho)*r.NormFloat64()*sd
		out[i] = level + e
	}
	return out
}

// An hour of strongly correlated noise (effective sample ~7) sometimes wanders
// like a real shift; the detector must stay quiet on at least 90% of them.
func TestDetectChangePoints_IgnoresAutocorrelatedNoise(t *testing.T) {
	const runs, maxFalseAlarms = 50, 5
	var alarms []int64
	for seed := int64(1); seed <= runs; seed++ {
		if got := detectChangePoints(ar1Noise(seed, 61, 36, 3, 0.8)); len(got) != 0 {
			alarms = append(alarms, seed)
		}
	}
	if len(alarms) > maxFalseAlarms {
		t.Errorf("%d of %d no-change series reported change points (seeds %v), want <= %d", len(alarms), runs, alarms, maxFalseAlarms)
	}
	shifted := append(ar1Noise(1, 30, 36, 3, 0.8), ar1Noise(2, 31, 72, 3, 0.8)...)
	if got := detectChangePoints(shifted); len(got) != 1 || got[0] < 27 || got[0] > 33 {
		t.Errorf("a doubling under the same noise must still be one change point near 30, got %v", got)
	}
}

// The relative-shift gate must hold for the means a change point reports,
// which span to its neighbouring change points, not only for the split tested.
func TestSummarizeSeries_ReportedChangePointsPassTheShiftGate(t *testing.T) {
	for seed := int64(1); seed <= 50; seed++ {
		values := ar1Noise(seed, 200, 36, 4, 0.3)
		for i := 100; i < len(values); i++ {
			values[i] *= 1.15
		}
		s := summarizeSeries(TimeSeries{Values: pointsFrom(values...)}, nil)
		if s.PerfSeriesStats == nil {
			t.Fatal("expected stats")
		}
		for _, cp := range s.ChangePoints {
			if math.Abs(cp.AfterMean-cp.BeforeMean) < perfSummaryMinRelativeShift*math.Max(math.Abs(cp.BeforeMean), math.Abs(cp.AfterMean)) {
				t.Errorf("seed %d: change point %+v shifts less than %v", seed, cp, perfSummaryMinRelativeShift)
			}
		}
	}
}

func TestSummarizeSeries_ChangePointMeans(t *testing.T) {
	values := []float64{10, 10, 10, 10, 50, 50, 50, 50, 50, 50}
	s := summarizeSeries(TimeSeries{Values: pointsFrom(values...)}, nil)
	want := []PerfChangePoint{{Timestamp: 1000 + 60*4, BeforeMean: 10, AfterMean: 50}}
	if !reflect.DeepEqual(s.ChangePoints, want) {
		t.Fatalf("change points = %+v, want %+v", s.ChangePoints, want)
	}
}

func TestSummarizeSeriesList_KeepsLargestAndCountsOmitted(t *testing.T) {
	var series []TimeSeries
	for i := 1; i <= perfSummaryMaxSeries+2; i++ {
		series = append(series, TimeSeries{Metric: map[string]string{"code": string(rune('a' + i))}, Values: pointsFrom(float64(i))})
	}
	got, omitted := summarizeSeriesList(series, nil, perfSummaryMaxSeries)
	if omitted != 2 || len(got) != perfSummaryMaxSeries {
		t.Fatalf("kept %d, omitted %d; want %d, 2", len(got), omitted, perfSummaryMaxSeries)
	}
	if got[0].Mean != float64(perfSummaryMaxSeries+2) || got[len(got)-1].Mean != 3 {
		t.Fatalf("expected the highest-mean series first and the two smallest dropped: %+v", got)
	}

	got, omitted = summarizeSeriesList(series[:perfSummaryMaxSeries], nil, perfSummaryMaxSeries)
	if omitted != 0 || len(got) != perfSummaryMaxSeries {
		t.Fatalf("at the cap: kept %d, omitted %d", len(got), omitted)
	}

	withEmpty := append([]TimeSeries{{Metric: map[string]string{"code": "empty"}}}, series[:2]...)
	got, _ = summarizeSeriesList(withEmpty, nil, perfSummaryMaxSeries)
	if got[len(got)-1].Metric["code"] != "empty" || got[0].Mean != 2 {
		t.Fatalf("a series without data must rank after every series with data: %+v", got)
	}
}

// Response times are one series per quantile, a fixed set: capping them by
// mean always dropped p50, the lowest and most-asked latency.
func TestSummarizePerformanceDetails_KeepsEveryResponseTimeQuantile(t *testing.T) {
	var d ServicePerformanceDetails
	for i, q := range []string{"max", "p99", "p95", "p90", "avg", "p50"} {
		d.ResponseTimes = append(d.ResponseTimes, TimeSeries{Metric: map[string]string{"quantile": q}, Values: pointsFrom(float64(100 - 10*i))})
	}
	s := summarizePerformanceDetails(d)
	if len(s.ResponseTimes) != len(d.ResponseTimes) || s.SeriesOmitted["response_times"] != 0 {
		t.Fatalf("kept %d of %d quantiles, series_omitted = %v", len(s.ResponseTimes), len(d.ResponseTimes), s.SeriesOmitted)
	}
	if last := s.ResponseTimes[len(s.ResponseTimes)-1].Metric["quantile"]; last != "p50" {
		t.Fatalf("lowest-mean quantile = %q, want p50", last)
	}
}
