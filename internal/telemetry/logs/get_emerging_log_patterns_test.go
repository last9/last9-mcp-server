package logs

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const precedingWindowStart = patternWindowStart - 3600

func sameLines(line string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = line
	}
	return out
}

func emergingArgs(scope LogPatternScopeArgs) GetEmergingLogPatternsArgs {
	scope.ServiceName = "checkout"
	scope.StartTimeISO = patternWindowStartISO
	scope.EndTimeISO = patternWindowEndISO
	return GetEmergingLogPatternsArgs{LogPatternScopeArgs: scope}
}

func callEmerging(t *testing.T, stub *logSearchStub, lines map[int64][]string, args GetEmergingLogPatternsArgs) (GetEmergingLogPatternsResponse, string) {
	t.Helper()
	srv := stub.serve(t, lines, nil)
	result, _, err := NewGetEmergingLogPatternsHandler(srv.Client(), apiModeCfg(srv.URL))(context.Background(), &mcp.CallToolRequest{}, args)
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	return decodeToolJSON[GetEmergingLogPatternsResponse](t, result), result.Content[0].(*mcp.TextContent).Text
}

func TestGetEmergingLogPatterns_ClassifiesChangesAgainstPrecedingWindow(t *testing.T) {
	current := append(sameLines("heartbeat ok", 5), "cache refreshed")
	current = append(current, sameLines("retrying connection to db", 8)...)
	current = append(current, repeatLines("payment declined for card %d", 6)...)
	baseline := append(sameLines("retrying connection to db", 2), sameLines("cache refreshed", 6)...)
	baseline = append(baseline, sameLines("heartbeat ok", 5)...)
	baseline = append(baseline, sameLines("worker started", 4)...)

	stub := &logSearchStub{}
	got, _ := callEmerging(t, stub, map[int64][]string{patternWindowStart: current, precedingWindowStart: baseline}, emergingArgs(LogPatternScopeArgs{}))

	if got.Baseline.StartTime != "2026-10-16T09:00:00Z" || got.Baseline.EndTime != "2026-10-16T10:00:00Z" {
		t.Errorf("baseline = %+v, want the preceding hour", got.Baseline)
	}
	if got.Current.SampledLines != 20 || got.Baseline.SampledLines != 17 {
		t.Errorf("sampled lines current=%d baseline=%d", got.Current.SampledLines, got.Baseline.SampledLines)
	}
	if got.ComparisonBasis != comparisonRatePerMinute {
		t.Errorf("comparison_basis = %q, want %q", got.ComparisonBasis, comparisonRatePerMinute)
	}

	type row struct {
		template, change  string
		current, baseline int
		ratio             float64
	}
	want := []row{
		{"payment declined for card <*>", changeNew, 6, 0, 0},
		{"retrying connection to db", changeIncreased, 8, 2, 4},
		{"cache refreshed", changeDecreased, 1, 6, 0.17},
		{"worker started", changeDisappeared, 0, 4, 0},
	}
	if len(got.Patterns) != len(want) {
		t.Fatalf("patterns = %+v, want %d", got.Patterns, len(want))
	}
	for i, w := range want {
		p := got.Patterns[i]
		ratio := 0.0
		if p.Ratio != nil {
			ratio = *p.Ratio
		}
		if p.Template != w.template || p.Change != w.change || p.Current.Count != w.current || p.Baseline.Count != w.baseline || ratio != w.ratio {
			t.Errorf("pattern %d = {%q %q current=%d baseline=%d ratio=%v}, want %+v", i, p.Template, p.Change, p.Current.Count, p.Baseline.Count, ratio, w)
		}
	}
	if got.UnchangedPatterns != 1 {
		t.Errorf("unchanged_patterns = %d, want 1 (heartbeat)", got.UnchangedPatterns)
	}
	if got.Patterns[0].Sample != "payment declined for card 0" || got.Patterns[3].Sample != "worker started" {
		t.Errorf("samples = %q / %q, want a line from the window the pattern appears in", got.Patterns[0].Sample, got.Patterns[3].Sample)
	}
	if got.Note != "" {
		t.Errorf("note = %q, want none", got.Note)
	}
	if n := len(stub.requests()); n != 2 {
		t.Errorf("requests = %d, want one per window", n)
	}
}

func TestGetEmergingLogPatterns_NoNoveltyReturnsEmptyPatterns(t *testing.T) {
	lines := append(repeatLines("GET /health 200 in %d ms", 30), repeatLines("job %d finished", 10)...)
	got, text := callEmerging(t, &logSearchStub{}, map[int64][]string{patternWindowStart: lines, precedingWindowStart: lines}, emergingArgs(LogPatternScopeArgs{}))

	if !strings.Contains(text, `"patterns": []`) {
		t.Errorf("no novelty must serve patterns as [], got:\n%s", text)
	}
	if got.UnchangedPatterns != 2 || got.Other != nil || got.Note != "" {
		t.Errorf("response = %+v, want 2 unchanged patterns and nothing else", got)
	}
}

func TestGetEmergingLogPatterns_IdenticalWindowsWithGeneralisingTemplatesHaveNoNovelty(t *testing.T) {
	lines := append(sameLines("request handled route=checkout status=ok region=west", 3), sameLines("request handled route=billing status=error region=west", 3)...)
	lines = append(lines, sameLines("request handled route=checkout status=ok region=east", 3)...)
	got, _ := callEmerging(t, &logSearchStub{}, map[int64][]string{patternWindowStart: lines, precedingWindowStart: lines}, emergingArgs(LogPatternScopeArgs{}))

	if len(got.Patterns) != 0 || got.UnchangedPatterns == 0 {
		t.Fatalf("identical windows must have no novelty; got patterns=%+v unchanged=%d", got.Patterns, got.UnchangedPatterns)
	}
}

func TestGetEmergingLogPatterns_ThresholdsAtTheirBoundaries(t *testing.T) {
	const unchanged, belowMinCount = "unchanged", "below min_count"
	cases := []struct {
		name              string
		current, baseline int
		want              string
	}{
		{"new at min_count", 3, 0, changeNew},
		{"new below min_count", 2, 0, belowMinCount},
		{"disappeared at min_count", 0, 3, changeDisappeared},
		{"disappeared below min_count", 0, 2, belowMinCount},
		{"increased at factor", 6, 3, changeIncreased},
		{"just below factor", 5, 3, unchanged},
		{"increased but current below min_count", 2, 1, belowMinCount},
		{"decreased at factor", 3, 6, changeDecreased},
		{"just above 1/factor", 4, 6, unchanged},
		{"decreased but baseline below min_count", 1, 2, belowMinCount},
		{"stable below min_count", 1, 1, unchanged},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			lines := map[int64][]string{
				patternWindowStart:   sameLines("queue depth alarm", c.current),
				precedingWindowStart: sameLines("queue depth alarm", c.baseline),
			}
			got, _ := callEmerging(t, &logSearchStub{}, lines, emergingArgs(LogPatternScopeArgs{}))
			outcome := ""
			switch {
			case len(got.Patterns) == 1 && got.UnchangedPatterns+got.BelowMinCountPatterns == 0:
				outcome = got.Patterns[0].Change
			case len(got.Patterns) == 0 && got.UnchangedPatterns == 1 && got.BelowMinCountPatterns == 0:
				outcome = unchanged
			case len(got.Patterns) == 0 && got.UnchangedPatterns == 0 && got.BelowMinCountPatterns == 1:
				outcome = belowMinCount
			}
			if outcome != c.want {
				t.Errorf("patterns=%+v unchanged=%d below_min_count=%d, want %q", got.Patterns, got.UnchangedPatterns, got.BelowMinCountPatterns, c.want)
			}
		})
	}
}

func TestGetEmergingLogPatterns_ComparesSharesWhenASampleIsCapped(t *testing.T) {
	lines := map[int64][]string{
		patternWindowStart:   sameLines("lock wait timeout", 4),
		precedingWindowStart: append(sameLines("lock wait timeout", 2), sameLines("vacuum done", 2)...),
	}
	args := emergingArgs(LogPatternScopeArgs{SampleSize: 4})
	args.MinCount = 2
	got, _ := callEmerging(t, &logSearchStub{}, lines, args)

	if got.ComparisonBasis != comparisonShareOfSample || !got.Current.SampleLimitReached {
		t.Fatalf("basis = %q current=%+v, want share_of_sample with the cap reached", got.ComparisonBasis, got.Current)
	}
	if len(got.Patterns) != 2 || got.Patterns[0].Change != changeIncreased || *got.Patterns[0].Ratio != 2 || got.Patterns[1].Change != changeDisappeared {
		t.Errorf("patterns = %+v, want lock wait increased x2 by share and vacuum disappeared", got.Patterns)
	}
}

// A capped dense window against a complete sparse one: the pattern's share of
// the sample halves while its rate grows 500x, so shares would report decreased.
func TestGetEmergingLogPatterns_ExtrapolatesACappedSampleWithAnExactTotal(t *testing.T) {
	stub := &logSearchStub{}
	srv := stub.serve(t, map[int64][]string{
		patternWindowStart:   append(sameLines("alarm raised", 5), sameLines("request served", 5)...),
		precedingWindowStart: sameLines("alarm raised", 10),
	}, map[int64]int{patternWindowStart: 10000, precedingWindowStart: 10})
	args := emergingArgs(LogPatternScopeArgs{SampleSize: 10})
	args.MinCount = 2
	result, _, err := NewGetEmergingLogPatternsHandler(srv.Client(), apiModeCfg(srv.URL))(context.Background(), &mcp.CallToolRequest{}, args)
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	got := decodeToolJSON[GetEmergingLogPatternsResponse](t, result)

	if !got.Current.SampleLimitReached || got.ComparisonBasis != comparisonEstimatedRatePerMinute {
		t.Fatalf("basis = %q current = %+v, want estimated_rate_per_minute with the current cap reached", got.ComparisonBasis, got.Current)
	}
	for _, p := range got.Patterns {
		if p.Template == "alarm raised" {
			if p.Change != changeIncreased || *p.Ratio != 500 {
				t.Fatalf("alarm = %+v, want increased x500 (5/10 of 10000 lines against 10)", p)
			}
			return
		}
	}
	t.Fatalf("patterns = %+v, want alarm raised reported", got.Patterns)
}

// A truncated search's total is only a floor, so it cannot scale a sample.
func TestGetEmergingLogPatterns_ComparesSharesWhenACappedTotalIsAFloor(t *testing.T) {
	stub := &logSearchStub{envelope: map[string]any{"logs_truncated": true}}
	srv := stub.serve(t, map[int64][]string{
		patternWindowStart:   sameLines("lock wait timeout", 4),
		precedingWindowStart: append(sameLines("lock wait timeout", 2), sameLines("vacuum done", 2)...),
	}, map[int64]int{patternWindowStart: 400, precedingWindowStart: 4})
	args := emergingArgs(LogPatternScopeArgs{SampleSize: 4})
	args.MinCount = 2
	result, _, err := NewGetEmergingLogPatternsHandler(srv.Client(), apiModeCfg(srv.URL))(context.Background(), &mcp.CallToolRequest{}, args)
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	got := decodeToolJSON[GetEmergingLogPatternsResponse](t, result)
	if got.ComparisonBasis != comparisonShareOfSample {
		t.Fatalf("basis = %q, want share_of_sample when the total is only a floor", got.ComparisonBasis)
	}
}

func TestGetEmergingLogPatterns_UsesExplicitBaselineAndNormalisesByDuration(t *testing.T) {
	baselineStart := patternWindowStart - 86400
	lines := map[int64][]string{
		patternWindowStart: sameLines("slow query", 4),
		baselineStart:      sameLines("slow query", 4),
	}
	args := emergingArgs(LogPatternScopeArgs{})
	args.BaselineStartTimeISO = "2026-10-15T10:00:00Z"
	args.BaselineEndTimeISO = "2026-10-15T12:00:00Z"
	stub := &logSearchStub{}
	got, _ := callEmerging(t, stub, lines, args)

	if got.Baseline.StartTime != "2026-10-15T10:00:00Z" || got.Baseline.EndTime != "2026-10-15T12:00:00Z" {
		t.Errorf("baseline = %+v, want the explicit window", got.Baseline)
	}
	if len(got.Patterns) != 1 || got.Patterns[0].Change != changeIncreased || *got.Patterns[0].Ratio != 2 {
		t.Errorf("patterns = %+v, want 4/h against 4 per 2h to be increased x2", got.Patterns)
	}
}

func TestGetEmergingLogPatterns_EmptyBaselineIsCalledOut(t *testing.T) {
	got, _ := callEmerging(t, &logSearchStub{}, map[int64][]string{patternWindowStart: sameLines("booting", 3)}, emergingArgs(LogPatternScopeArgs{}))

	if len(got.Patterns) != 1 || got.Patterns[0].Change != changeNew {
		t.Errorf("patterns = %+v, want the one pattern reported as new", got.Patterns)
	}
	if !strings.Contains(got.Note, "Baseline window has no matching lines") {
		t.Errorf("note = %q, want the empty baseline called out", got.Note)
	}
}

func TestGetEmergingLogPatterns_TruncatedEmptyBaselineIsCalledOutAsIncomplete(t *testing.T) {
	stub := &logSearchStub{envelope: map[string]any{"logs_truncated": true}}
	got, _ := callEmerging(t, stub, map[int64][]string{patternWindowStart: sameLines("booting", 3)}, emergingArgs(LogPatternScopeArgs{}))

	if !got.Baseline.SampleLimitReached || got.Baseline.SampledLines != 0 {
		t.Fatalf("baseline = %+v, want a truncated empty sample", got.Baseline)
	}
	if strings.Contains(got.Note, "no matching lines") {
		t.Errorf("note = %q, must not claim no matching lines for a truncated baseline", got.Note)
	}
	if !strings.Contains(got.Note, "Baseline sample is empty but incomplete") {
		t.Errorf("note = %q, want the incomplete baseline sample called out", got.Note)
	}
}

func TestGetEmergingLogPatterns_BoundsOutputWithOther(t *testing.T) {
	current := append(sameLines("gamma failed right now", 3), sameLines("beta failed now", 4)...)
	current = append(current, sameLines("alpha failed", 5)...)
	args := emergingArgs(LogPatternScopeArgs{})
	args.TopN = 1
	got, _ := callEmerging(t, &logSearchStub{}, map[int64][]string{patternWindowStart: current, precedingWindowStart: {"unrelated line here and there", "beta failed now"}}, args)

	if len(got.Patterns) != 1 || got.Patterns[0].Template != "alpha failed" {
		t.Errorf("patterns = %+v, want only the largest new pattern", got.Patterns)
	}
	if got.Other == nil || got.Other.Patterns != 2 || got.Other.CurrentCount != 7 || got.Other.BaselineCount != 1 {
		t.Errorf("other = %+v, want 2 more significant patterns holding 7 current and 1 baseline line", got.Other)
	}
}

func TestGetEmergingLogPatterns_RejectsInvalidArgsBeforeQuerying(t *testing.T) {
	withBaseline := func(start, end string) GetEmergingLogPatternsArgs {
		args := emergingArgs(LogPatternScopeArgs{})
		args.BaselineStartTimeISO, args.BaselineEndTimeISO = start, end
		return args
	}
	withOption := func(set func(*GetEmergingLogPatternsArgs)) GetEmergingLogPatternsArgs {
		args := emergingArgs(LogPatternScopeArgs{})
		set(&args)
		return args
	}
	cases := map[string]GetEmergingLogPatternsArgs{
		"missing service":           {},
		"baseline start only":       withBaseline("2026-10-15T10:00:00Z", ""),
		"baseline end only":         withBaseline("", "2026-10-15T11:00:00Z"),
		"baseline unparseable":      withBaseline("yesterday", "2026-10-15T11:00:00Z"),
		"baseline empty range":      withBaseline("2026-10-15T11:00:00Z", "2026-10-15T11:00:00Z"),
		"baseline overlaps current": withBaseline("2026-10-16T09:30:00Z", "2026-10-16T10:30:00Z"),
		"change_factor of 1":        withOption(func(a *GetEmergingLogPatternsArgs) { a.ChangeFactor = 1 }),
		"negative change_factor":    withOption(func(a *GetEmergingLogPatternsArgs) { a.ChangeFactor = -2 }),
		"negative min_count":        withOption(func(a *GetEmergingLogPatternsArgs) { a.MinCount = -1 }),
		"min_count over max":        withOption(func(a *GetEmergingLogPatternsArgs) { a.MinCount = maxPatternSampleSize + 1 }),
		"min_count over sample_size": withOption(func(a *GetEmergingLogPatternsArgs) {
			a.SampleSize, a.MinCount = 10, 11
		}),
		"min_count over default sample_size": withOption(func(a *GetEmergingLogPatternsArgs) {
			a.MinCount = defaultPatternSampleSize + 1
		}),
		"top_n over max": withOption(func(a *GetEmergingLogPatternsArgs) { a.TopN = maxPatternTopN + 1 }),
		"current empty range": withOption(func(a *GetEmergingLogPatternsArgs) {
			a.StartTimeISO, a.EndTimeISO = patternWindowStartISO, patternWindowStartISO
		}),
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			stub := &logSearchStub{}
			srv := stub.serve(t, nil, nil)
			_, _, err := NewGetEmergingLogPatternsHandler(srv.Client(), apiModeCfg(srv.URL))(context.Background(), &mcp.CallToolRequest{}, args)
			if err == nil {
				t.Fatal("want an error")
			}
			if n := len(stub.requests()); n != 0 {
				t.Errorf("made %d requests before rejecting", n)
			}
		})
	}
}

func TestGetEmergingLogPatterns_MinCountErrorBlamesSampleSizeOnlyWhenOverIt(t *testing.T) {
	const blame = "min_count cannot exceed sample_size"
	cases := map[int]bool{-1: false, 11: true, maxPatternSampleSize + 1: true}
	for minCount, wantBlame := range cases {
		_, err := GetEmergingLogPatternsArgs{MinCount: minCount}.thresholds(10)
		if err == nil {
			t.Fatalf("min_count %d: want an error", minCount)
		}
		if got := strings.Contains(err.Error(), blame); got != wantBlame {
			t.Errorf("min_count %d: error %q mentions %q = %v, want %v", minCount, err, blame, got, wantBlame)
		}
	}
}

func TestGetEmergingLogPatterns_MinCountMayEqualSampleSize(t *testing.T) {
	args := emergingArgs(LogPatternScopeArgs{SampleSize: 4})
	args.MinCount = 4
	got, _ := callEmerging(t, &logSearchStub{}, map[int64][]string{patternWindowStart: sameLines("disk full", 4)}, args)
	if len(got.Patterns) != 1 || got.Patterns[0].Change != changeNew {
		t.Fatalf("patterns = %+v, want a pattern filling the sample reported as new", got.Patterns)
	}
}

func TestGetEmergingLogPatterns_DefaultMinCountFitsASmallSample(t *testing.T) {
	got, _ := callEmerging(t, &logSearchStub{}, map[int64][]string{patternWindowStart: sameLines("disk full", 2)}, emergingArgs(LogPatternScopeArgs{SampleSize: 2}))
	if len(got.Patterns) != 1 || got.Patterns[0].Change != changeNew {
		t.Fatalf("patterns = %+v, want the default min_count lowered to sample_size 2", got.Patterns)
	}
}

func TestGetEmergingLogPatterns_BaselineMayEndExactlyWhereCurrentStarts(t *testing.T) {
	args := emergingArgs(LogPatternScopeArgs{})
	args.BaselineStartTimeISO, args.BaselineEndTimeISO = "2026-10-16T09:00:00Z", patternWindowStartISO
	stub := &logSearchStub{}
	callEmerging(t, stub, nil, args)
	if n := len(stub.requests()); n != 2 {
		t.Errorf("requests = %d, want both windows queried", n)
	}
}

func TestGetEmergingLogPatterns_FetchesBothWindowsConcurrently(t *testing.T) {
	var arrived sync.WaitGroup
	arrived.Add(2)
	bothInFlight := make(chan struct{})
	go func() { arrived.Wait(); close(bothInFlight) }()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		arrived.Done()
		select {
		case <-bothInFlight:
		case <-time.After(5 * time.Second):
			http.Error(w, "windows were fetched one after the other", http.StatusGatewayTimeout)
			return
		}
		_, _ = w.Write([]byte(`{"query_result":{"status":"success","data":{"resultType":"streams","result":[]}}}`))
	}))
	t.Cleanup(srv.Close)

	_, _, err := NewGetEmergingLogPatternsHandler(srv.Client(), apiModeCfg(srv.URL))(context.Background(), &mcp.CallToolRequest{}, emergingArgs(LogPatternScopeArgs{}))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
}

func TestGetEmergingLogPatterns_ExactFillKeepsRateBasisAndReportsIncrease(t *testing.T) {
	stub := &logSearchStub{}
	srv := stub.serve(t, map[int64][]string{
		patternWindowStart:   sameLines("lock wait timeout", 4),
		precedingWindowStart: sameLines("lock wait timeout", 2),
	}, map[int64]int{patternWindowStart: 4, precedingWindowStart: 2})
	args := emergingArgs(LogPatternScopeArgs{SampleSize: 4})
	args.MinCount = 2
	result, _, err := NewGetEmergingLogPatternsHandler(srv.Client(), apiModeCfg(srv.URL))(context.Background(), &mcp.CallToolRequest{}, args)
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	got := decodeToolJSON[GetEmergingLogPatternsResponse](t, result)
	if got.Current.SampleLimitReached || got.ComparisonBasis != comparisonRatePerMinute {
		t.Fatalf("basis = %q current = %+v; a complete exact fill must stay rate_per_minute", got.ComparisonBasis, got.Current)
	}
	if len(got.Patterns) != 1 || got.Patterns[0].Change != changeIncreased || *got.Patterns[0].Ratio != 2 {
		t.Fatalf("patterns = %+v, want lock wait increased x2 by rate", got.Patterns)
	}
}

func TestGetEmergingLogPatterns_RejectsAWindowWithFailedChunks(t *testing.T) {
	stub := &logSearchStub{envelope: map[string]any{"search_stats": map[string]any{"chunks_failed": 2}}}
	srv := stub.serve(t, map[int64][]string{patternWindowStart: sameLines("booting", 3)}, nil)
	_, _, err := NewGetEmergingLogPatternsHandler(srv.Client(), apiModeCfg(srv.URL))(context.Background(), &mcp.CallToolRequest{}, emergingArgs(LogPatternScopeArgs{}))
	if err == nil || !strings.Contains(err.Error(), "2 of its search chunks failed") {
		t.Fatalf("err = %v, want the incomplete window rejected", err)
	}
}
