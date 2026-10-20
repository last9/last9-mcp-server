package changeimpact

import (
	"net/http"
	"strings"
	"testing"

	"last9-mcp/internal/apm"
)

// bandOf is one window per p95, each with 1000 requests and the given errors.
func bandOf(p95s ...float64) []apm.GoldenSignals {
	band := make([]apm.GoldenSignals, len(p95s))
	for i, p := range p95s {
		band[i] = signals(1000, 5, p)
	}
	return band
}

func errorBandOf(errorPcts ...float64) []apm.GoldenSignals {
	band := make([]apm.GoldenSignals, len(errorPcts))
	for i, e := range errorPcts {
		band[i] = signals(1000, e*10, 100)
	}
	return band
}

var (
	// spikyP95 moves ±30% around 100 ms with no change: median 100, MAD 20.
	spikyP95  = []float64{100, 130, 75, 120, 90}
	stableP95 = []float64{100, 101, 99, 100, 100}
)

func TestBandJudgesP95AgainstTheServicesOwnNoise(t *testing.T) {
	cases := []struct {
		name         string
		band         []apm.GoldenSignals
		after        float64
		wantRegress  bool
		wantPossible bool
	}{
		{"spiky, no change", bandOf(spikyP95...), 125, false, false},
		{"spiky +28%, inside the range", bandOf(spikyP95...), 128, false, false},
		{"spiky, above the range but within noise", bandOf(spikyP95...), 150, false, true},
		{"spiky at median + 3·MAD", bandOf(spikyP95...), 160, false, true},
		{"spiky +80%", bandOf(spikyP95...), 180, true, false},
		{"stable +25%", bandOf(stableP95...), 125, true, false},
		{"stable at the minimum move", bandOf(stableP95...), 115, true, false},
		{"stable under the minimum move", bandOf(stableP95...), 114.9, false, false},
		{"stable +10%", bandOf(stableP95...), 110, false, false},
		{"improvement", bandOf(stableP95...), 80, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := serviceWindows{baseline: signals(1000, 5, 100), after: signals(1000, 5, tc.after), band: tc.band, bandMinutes: 60}
			regressions, possible := serviceFindings("checkout", w)
			if got := len(regressions) > 0; got != tc.wantRegress {
				t.Fatalf("regressions = %q, want regression %v", regressions, tc.wantRegress)
			}
			if got := len(possible) > 0; got != tc.wantPossible {
				t.Fatalf("possible = %q, want possible %v", possible, tc.wantPossible)
			}
		})
	}
}

func TestBandJudgesErrorRateAgainstTheServicesOwnNoise(t *testing.T) {
	// Spiky errors: median 1.5%, MAD 1 pp, so the noise ceiling is 4.5%.
	spiky := errorBandOf(1, 4, 0.5, 3, 1.5)
	cases := []struct {
		name         string
		band         []apm.GoldenSignals
		after        float64
		wantRegress  bool
		wantPossible bool
	}{
		{"flat 0.5%, +1.5 pp", errorBandOf(0.5, 0.5, 0.5, 0.5, 0.5), 2, true, false},
		{"flat 0.5%, +0.9 pp", errorBandOf(0.5, 0.5, 0.5, 0.5, 0.5), 1.4, false, false},
		{"spiky, inside the range", spiky, 3, false, false},
		{"spiky, above the range but within noise", spiky, 4.2, false, true},
		{"spiky, past the noise", spiky, 6, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := serviceWindows{baseline: signals(1000, 5, 100), after: signals(1000, tc.after*10, 100), band: tc.band, bandMinutes: 60}
			regressions, possible := serviceFindings("checkout", w)
			if got := len(regressions) > 0; got != tc.wantRegress {
				t.Fatalf("regressions = %q, want regression %v", regressions, tc.wantRegress)
			}
			if got := len(possible) > 0; got != tc.wantPossible {
				t.Fatalf("possible = %q, want possible %v", possible, tc.wantPossible)
			}
		})
	}
}

// Windows under the request minimum do not count toward the band; with fewer
// than bandMinWindows left, the fixed threshold judges the after window.
func TestThinBandWindowsFallBackToTheFixedThreshold(t *testing.T) {
	thin := bandOf(stableP95...)
	for i := range len(thin) - (bandMinWindows - 1) {
		thin[i] = signals(minRequestsPerWindow-1, 0, 100)
	}
	w := serviceWindows{baseline: signals(1000, 5, 100), after: signals(1000, 5, 125), band: thin, bandMinutes: 60}
	if regressions, possible := serviceFindings("checkout", w); len(regressions)+len(possible) != 0 {
		t.Fatalf("with %d qualifying windows, +25%% must be judged by the fixed %g%% threshold; got %q %q", bandMinWindows-1, p95LatencyIncreasePct, regressions, possible)
	}
	if band := newBand(p95Values(w.band, "prod"), 60); band == nil || band.Windows != bandMinWindows-1 {
		t.Fatalf("band = %+v, want %d windows", band, bandMinWindows-1)
	}

	enough := bandOf(stableP95...)
	enough[0] = signals(minRequestsPerWindow-1, 0, 100)
	enough[1] = signals(minRequestsPerWindow-1, 0, 100)
	w.band = enough
	if regressions, _ := serviceFindings("checkout", w); len(regressions) != 1 {
		t.Fatalf("with %d qualifying windows, +25%% on a stable service is a regression; got %q", bandMinWindows, regressions)
	}
}

// The agent explains the call from the reason: it quotes the band.
func TestBandFindingsQuoteTheBand(t *testing.T) {
	w := serviceWindows{baseline: signals(1000, 5, 100), after: signals(1000, 5, 180), band: bandOf(spikyP95...), bandMinutes: 60}
	regressions, _ := serviceFindings("checkout", w)
	want := "checkout p95 latency 180.0 ms in prod vs baseline range 75.0 ms–130.0 ms over 5 windows of 60 min (threshold: above the range, median 100.0 ms + 3×MAD 20.0 ms = 160.0 ms, and +15% over the median)"
	if len(regressions) != 1 || regressions[0] != want {
		t.Fatalf("regressions = %q, want [%q]", regressions, want)
	}

	w.after = signals(1000, 5, 150)
	_, possible := serviceFindings("checkout", w)
	want = "checkout p95 latency 150.0 ms in prod vs baseline range 75.0 ms–130.0 ms over 5 windows of 60 min: above the range but within noise (median 100.0 ms + 3×MAD 20.0 ms = 160.0 ms)"
	if len(possible) != 1 || possible[0] != want {
		t.Fatalf("possible = %q, want [%q]", possible, want)
	}
}

func TestBandMedianAndMAD(t *testing.T) {
	cases := []struct {
		values              []float64
		median, mad, lo, hi float64
	}{
		{spikyP95, 100, 20, 75, 130},
		{[]float64{1, 2, 3, 10}, 2.5, 1, 1, 10},
		{[]float64{7}, 7, 0, 7, 7},
	}
	for _, tc := range cases {
		b := newBand(tc.values, 30)
		if b.Median != tc.median || b.MAD != tc.mad || b.Min != tc.lo || b.Max != tc.hi || b.Windows != len(tc.values) || b.WindowMinutes != 30 {
			t.Fatalf("newBand(%v) = %+v", tc.values, b)
		}
	}
	if newBand(nil, 30) != nil {
		t.Fatal("no qualifying window means no band")
	}
}

func TestPossibleChangeVerdict(t *testing.T) {
	flat := signals(1000, 5, 100)
	base := verdictInputs{afterMinutes: 60, haveChanged: true, changedBaseline: flat, changedAfter: flat, possible: []string{"checkout p95 latency 150.0 ms"}}

	verdict, reason, note := decideVerdict(base)
	if verdict != VerdictPossibleChange || !strings.Contains(reason, "checkout p95 latency 150.0 ms") || note != "" {
		t.Fatalf("verdict = %q (%s, note %q), want %q citing the finding", verdict, reason, note, VerdictPossibleChange)
	}

	withTrigger := base
	withTrigger.triggers = []string{"new alert"}
	if verdict, _, _ := decideVerdict(withTrigger); verdict != VerdictRegressionLikely {
		t.Fatalf("a regression outranks a possible change; got %q", verdict)
	}

	withGap := base
	withGap.unavailable = []string{"alerts unavailable: boom"}
	if verdict, reason, _ := decideVerdict(withGap); verdict != VerdictPossibleChange || !strings.Contains(reason, "alerts unavailable") {
		t.Fatalf("verdict = %q (%s), want a possible change naming what failed", verdict, reason)
	}
}

func bandWindowsOf(p95s ...float64) []windowData {
	band := make([]windowData, len(p95s))
	for i, p := range p95s {
		band[i] = flatWindow()
		band[i].p95 = map[string]float64{"checkout": p, "web": 80}
	}
	return band
}

func TestAnalyzeJudgesP95AgainstTheBandBeforeTheChange(t *testing.T) {
	cases := []struct {
		name        string
		band        []float64
		after       float64
		wantVerdict string
	}{
		// The fixed 40% threshold would miss this: the service is stable.
		{"stable +25%", stableP95, 125, VerdictRegressionLikely},
		// The fixed threshold would flag this: the service is spiky.
		{"spiky +45%", spikyP95, 145, VerdictPossibleChange},
		{"spiky +28%", spikyP95, 128, VerdictNoSignificantChange},
		{"spiky +80%", spikyP95, 180, VerdictRegressionLikely},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			change := recentChange()
			after := flatWindow()
			after.p95 = map[string]float64{"checkout": tc.after, "web": 80}
			f := &fakeLast9{change: change, baseline: flatWindow(), after: after, band: bandWindowsOf(tc.band...)}

			report, err := runTool(t, f, serviceArgs(change))
			if err != nil {
				t.Fatal(err)
			}
			if report.Verdict != tc.wantVerdict {
				t.Fatalf("verdict = %q (%s), want %q", report.Verdict, report.Reason, tc.wantVerdict)
			}
			band := signalsFor(t, report, "checkout").LatencyP95Ms.BaselineBand
			if band == nil || band.Windows != bandWindows || band.WindowMinutes != report.Windows.AfterMinutes {
				t.Fatalf("p95 baseline_band = %+v, want %d windows of %d min", band, bandWindows, report.Windows.AfterMinutes)
			}
			// Four count classes and the latency query, each once for all band windows.
			if got := f.count("prom_range"); got != 5 {
				t.Fatalf("range queries = %d, want 5: one per golden-signal query, not one per band window", got)
			}
		})
	}
}

// Without the band the fixed thresholds still judge, and the report says so.
func TestFailingBandFallsBackToFixedThresholds(t *testing.T) {
	change := recentChange()
	after := flatWindow()
	after.p95 = map[string]float64{"checkout": 145, "web": 80}
	f := &fakeLast9{change: change, baseline: flatWindow(), after: after, band: bandWindowsOf(spikyP95...), fail: map[string]int{"prom_range": http.StatusBadGateway}}

	report, err := runTool(t, f, serviceArgs(change))
	if err != nil {
		t.Fatal(err)
	}
	if report.Verdict != VerdictRegressionLikely || !strings.Contains(report.Reason, "threshold 40%") {
		t.Fatalf("verdict = %q (%s), want the fixed p95 threshold", report.Verdict, report.Reason)
	}
	if !strings.Contains(strings.Join(report.Notes, "; "), "baseline band unavailable") {
		t.Fatalf("notes = %q, want the band failure named", report.Notes)
	}
	if band := signalsFor(t, report, "checkout").LatencyP95Ms.BaselineBand; band != nil {
		t.Fatalf("baseline_band = %+v, want none", band)
	}
}
