package logs

import (
	"fmt"
	"strings"
	"testing"
)

func templates(patterns []*logPattern) []string {
	out := make([]string, 0, len(patterns))
	for _, p := range patterns {
		out = append(out, p.template())
	}
	return out
}

func TestLogPatternMiner_MergesLinesThatDifferOnlyInVariables(t *testing.T) {
	lines := []LogEntry{
		{Timestamp: "2026-10-16T10:00:00Z", Message: "user 42 logged in from 10.0.0.1", Severity: "INFO"},
		{Timestamp: "2026-10-16T10:01:00Z", Message: "user 7 logged in from 10.0.0.2:8080", Severity: "INFO"},
		{Timestamp: "2026-10-16T10:02:00Z", Message: "request 3f2a1b9c-1d2e-4f50-9a7b-0c1d2e3f4a5b failed after 2026-10-16T10:01:59Z", Severity: "ERROR"},
		{Timestamp: "2026-10-16T10:03:00Z", Message: "request 0b1c2d3e-4f50-4a6b-8c7d-9e0f1a2b3c4d failed after 2026-10-16T10:02:59.123Z", Severity: "ERROR"},
	}

	got := templates(mineLogPatterns(lines))
	want := []string{"user <*> logged in from <*>", "request <*> failed after <*>"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("templates = %q, want %q", got, want)
	}
}

func TestLogPatternMiner_GeneralisesDifferingTokensWithinOnePattern(t *testing.T) {
	lines := []LogEntry{
		{Message: "connection refused to host db-primary"},
		{Message: "connection refused to host db-replica"},
	}

	got := templates(mineLogPatterns(lines))
	if len(got) != 1 || got[0] != "connection refused to host <*>" {
		t.Fatalf("templates = %q, want one generalised template", got)
	}
}

func TestLogPatternMiner_KeepsDistinctMessagesApart(t *testing.T) {
	lines := []LogEntry{
		{Message: "payment captured"},
		{Message: "cache warmed"},
		{Message: "GET /api/users 200"},
		{Message: "POST /api/orders 500"},
	}

	got := templates(mineLogPatterns(lines))
	if len(got) != 4 {
		t.Fatalf("templates = %q, want 4 distinct patterns", got)
	}
}

func TestLogPatternMiner_ClustersJSONBodiesOnTheirMessage(t *testing.T) {
	lines := []LogEntry{
		{Message: `{"level":"error","msg":"order 981 rejected","ts":1760608800}`},
		{Message: `{"level":"error","msg":"order 982 rejected","ts":1760608801}`},
		{Message: `{"level":"info","message":"cart saved"}`},
		{Message: `{"level":"info","detail":"no message key"}`},
	}

	got := templates(mineLogPatterns(lines))
	want := []string{"order <*> rejected", "cart saved", `{"level":"info","detail":"no message key"}`}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("templates = %q, want %q", got, want)
	}
}

func TestLogPatternMiner_TracksStatsPerWindow(t *testing.T) {
	baseline := []LogEntry{
		{Timestamp: "2026-10-16T09:10:00Z", Message: "retrying call 1", Severity: "WARN"},
	}
	current := []LogEntry{
		{Timestamp: "2026-10-16T10:05:00Z", Message: "retrying call 2", Severity: "WARN"},
		{Timestamp: "2026-10-16T10:01:00Z", Message: "retrying call 3", Severity: "ERROR"},
		{Timestamp: "2026-10-16T10:09:00Z", Message: "retrying call 4"},
	}

	patterns := mineLogPatterns(baseline, current)
	if len(patterns) != 1 {
		t.Fatalf("templates = %q, want one", templates(patterns))
	}
	p := patterns[0]
	if p.sample != "retrying call 1" {
		t.Errorf("sample = %q, want the first line seen", p.sample)
	}

	base := p.stats(0)
	if base.Count != 1 || base.FirstSeen != "2026-10-16T09:10:00Z" || base.LastSeen != "2026-10-16T09:10:00Z" {
		t.Errorf("baseline stats = %+v", base)
	}
	cur := p.stats(1)
	if cur.Count != 3 || cur.FirstSeen != "2026-10-16T10:01:00Z" || cur.LastSeen != "2026-10-16T10:09:00Z" {
		t.Errorf("current stats = %+v, want count 3 spanning 10:01..10:09", cur)
	}
	if fmt.Sprint(cur.SeverityCounts) != "map[ERROR:1 WARN:1 unknown:1]" {
		t.Errorf("current severity_counts = %v", cur.SeverityCounts)
	}
}

func TestLogPatternMiner_NamesTheEmptyMessagePattern(t *testing.T) {
	got := templates(mineLogPatterns([]LogEntry{{Message: ""}, {Message: "   "}}))
	if len(got) != 1 || got[0] != emptyPatternTemplate {
		t.Fatalf("templates = %q, want one %q pattern", got, emptyPatternTemplate)
	}
}

// Templates generalise while lines are still arriving, so a line counted
// early could land elsewhere than an identical line counted late. Identical
// windows must still produce identical counts.
func TestLogPatternMiner_CountsIdenticalWindowsIdentically(t *testing.T) {
	lines := append(sameEntries("request handled route=checkout status=ok region=west", 3), sameEntries("request handled route=billing status=error region=west", 3)...)
	lines = append(lines, sameEntries("request handled route=checkout status=ok region=east", 3)...)

	patterns := mineLogPatterns(lines, lines)
	total := 0
	for _, p := range patterns {
		current, baseline := p.stats(0).Count, p.stats(1).Count
		if current != baseline || current == 0 {
			t.Errorf("%q counted current=%d baseline=%d, want equal and non-zero", p.template(), current, baseline)
		}
		total += current
	}
	if total != len(lines) {
		t.Errorf("counted %d lines per window across %q, want all %d", total, templates(patterns), len(lines))
	}
	// A line fitting several templates goes to the most specific one, so the
	// checkout lines stay together rather than in the broader west template.
	for _, p := range patterns {
		if p.template() == "request handled route=checkout status=ok <*>" && p.stats(0).Count != 6 {
			t.Errorf("checkout template holds %d lines, want 6", p.stats(0).Count)
		}
	}
}

// The first template learned here generalises past its lines, which all end
// up in more specific templates; it must not be served with a zero count.
func TestLogPatternMiner_DropsTemplatesLeftWithoutLines(t *testing.T) {
	var lines []LogEntry
	for _, m := range []string{"x y b c b c b", "x y c b a c b", "x y c b b c b", "x y c c b b b", "x y a c b c b", "x y c b c c c", "x y c b a c a", "x y a c c b c"} {
		lines = append(lines, LogEntry{Message: m})
	}

	patterns := mineLogPatterns(lines)
	total := 0
	for _, p := range patterns {
		if p.stats(0).Count == 0 {
			t.Errorf("%q served with no lines", p.template())
		}
		total += p.stats(0).Count
	}
	if total != len(lines) {
		t.Errorf("counted %d lines across %q, want %d", total, templates(patterns), len(lines))
	}
}

func TestLogPatternMiner_WildcardSlotsKeepMatchingLaterVariants(t *testing.T) {
	cases := [][]string{
		{"connection refused to host db-primary", "connection refused to host db-replica", "connection refused to host db-canary"},
		{"connection refused to host db-primary after retry", "connection refused to host db-replica after retry", "connection refused to host db-canary after retry"},
	}
	for _, messages := range cases {
		lines := make([]LogEntry, 0, len(messages))
		for _, m := range messages {
			lines = append(lines, LogEntry{Message: m})
		}
		patterns := mineLogPatterns(lines)
		if len(patterns) != 1 || patterns[0].stats(0).Count != 3 {
			t.Errorf("templates = %q, want one generalised host pattern holding all 3 lines", templates(patterns))
		}
	}
}

func sameEntries(message string, n int) []LogEntry {
	out := make([]LogEntry, n)
	for i := range out {
		out[i] = LogEntry{Message: message}
	}
	return out
}

func TestLogPatternMiner_TruncatesLongSamples(t *testing.T) {
	long := strings.Repeat("x", maxPatternSampleRunes+50)
	p := mineLogPatterns([]LogEntry{{Message: long}})[0]
	if got := len([]rune(p.sample)); got != maxPatternSampleRunes+1 {
		t.Fatalf("sample length = %d runes, want %d plus an ellipsis", got, maxPatternSampleRunes)
	}
}

func TestRankLogPatterns_BreaksTiesByTemplateAndSplitsPastTopN(t *testing.T) {
	type item struct {
		template string
		count    int
	}
	byCountDesc := func(a, b item) int { return b.count - a.count }
	name := func(i item) string { return i.template }
	join := func(xs []item) string {
		out := make([]string, 0, len(xs))
		for _, x := range xs {
			out = append(out, x.template)
		}
		return strings.Join(out, " ")
	}

	for _, tc := range []struct {
		topN              int
		wantTop, wantRest string
	}{
		{topN: 3, wantTop: "c d a", wantRest: "b"},
		{topN: 4, wantTop: "c d a b", wantRest: ""},
		{topN: 5, wantTop: "c d a b", wantRest: ""},
	} {
		// The tied "a" and "b" arrive out of template order, so the tie-break must reorder them.
		items := []item{{"b", 1}, {"c", 5}, {"a", 1}, {"d", 3}}
		top, rest := rankLogPatterns(items, tc.topN, name, byCountDesc)
		if got := join(top); got != tc.wantTop {
			t.Errorf("topN=%d: top = %q, want %q", tc.topN, got, tc.wantTop)
		}
		if got := join(rest); got != tc.wantRest {
			t.Errorf("topN=%d: rest = %q, want %q", tc.topN, got, tc.wantRest)
		}
	}
}
