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

func mustMineLogPatterns(t *testing.T, windows ...[]LogEntry) []*logPattern {
	t.Helper()
	patterns, err := mineLogPatterns(windows...)
	if err != nil {
		t.Fatalf("mineLogPatterns: %v", err)
	}
	return patterns
}

func TestLogPatternMiner_MergesLinesThatDifferOnlyInVariables(t *testing.T) {
	lines := []LogEntry{
		{Timestamp: "2026-10-16T10:00:00Z", Message: "user 42 logged in from 10.0.0.1", Severity: "INFO"},
		{Timestamp: "2026-10-16T10:01:00Z", Message: "user 7 logged in from 10.0.0.2:8080", Severity: "INFO"},
		{Timestamp: "2026-10-16T10:02:00Z", Message: "request 3f2a1b9c-1d2e-4f50-9a7b-0c1d2e3f4a5b failed after 2026-10-16T10:01:59Z", Severity: "ERROR"},
		{Timestamp: "2026-10-16T10:03:00Z", Message: "request 0b1c2d3e-4f50-4a6b-8c7d-9e0f1a2b3c4d failed after 2026-10-16T10:02:59.123Z", Severity: "ERROR"},
	}

	got := templates(mustMineLogPatterns(t, lines))
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

	got := templates(mustMineLogPatterns(t, lines))
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

	got := templates(mustMineLogPatterns(t, lines))
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

	got := templates(mustMineLogPatterns(t, lines))
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

	patterns := mustMineLogPatterns(t, baseline, current)
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

func TestLogPatternMiner_SeenTimesSkipUnparseableTimestamps(t *testing.T) {
	patterns := mustMineLogPatterns(t,
		[]LogEntry{{Timestamp: "not-a-time", Message: "retrying call 1"}, {Message: "retrying call 2"}},
		[]LogEntry{
			{Timestamp: "not-a-time", Message: "retrying call 3"},
			{Timestamp: "2026-10-16T10:01:00Z", Message: "retrying call 4"},
		},
	)
	if len(patterns) != 1 {
		t.Fatalf("templates = %q, want one", templates(patterns))
	}
	if got := patterns[0].stats(0); got.Count != 2 || got.FirstSeen != "" || got.LastSeen != "" {
		t.Errorf("stats with no parseable timestamp = %+v, want count 2 and no seen times", got)
	}
	if got := patterns[0].stats(1); got.Count != 2 || got.FirstSeen != "2026-10-16T10:01:00Z" || got.LastSeen != "2026-10-16T10:01:00Z" {
		t.Errorf("stats = %+v, want count 2 and seen times only from the parseable line", got)
	}
}

func TestLogPatternMiner_NamesTheEmptyMessagePattern(t *testing.T) {
	got := templates(mustMineLogPatterns(t, []LogEntry{{Message: ""}, {Message: "   "}}))
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

	patterns := mustMineLogPatterns(t, lines, lines)
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

	patterns := mustMineLogPatterns(t, lines)
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
		patterns := mustMineLogPatterns(t, lines)
		if len(patterns) != 1 || patterns[0].stats(0).Count != 3 {
			t.Errorf("templates = %q, want one generalised host pattern holding all 3 lines", templates(patterns))
		}
	}
}

// A line no frozen template fits cannot happen today; if a mask or routing
// change ever breaks that, counting must fail rather than panic or drop it.
func TestLogPatternMiner_CountFailsOnALineNoTemplateFits(t *testing.T) {
	miner := logPatternMiner{groups: map[string][]*logPattern{}}
	miner.learn(patternTokens("cache warmed"))

	windows := [][]LogEntry{{{Message: "cache missed"}}}
	err := miner.count(windows, [][][]string{{patternTokens("cache missed")}})
	if err == nil || !strings.Contains(err.Error(), "fits no log pattern") {
		t.Fatalf("err = %v, want a fits-no-log-pattern error", err)
	}
}

func TestLogPatternTokens_DottedQuadsMaskWhetherIPOrVersion(t *testing.T) {
	for _, message := range []string{"deployed build 1.2.3.4", "deployed build 10.20.30.40:8080"} {
		if got := strings.Join(patternTokens(message), " "); got != "deployed build <*>" {
			t.Errorf("patternTokens(%q) = %q, want the dotted quad as one wildcard", message, got)
		}
	}
	// Without the dotted-quad mask a version still loses every digit, so
	// versions would merge into one template either way.
	if got := strings.Join(patternTokens("deployed build 1.2.3"), " "); got != "deployed build <*>.<*>" {
		t.Errorf("three-part version = %q, want only its digits wildcarded", got)
	}
}

func sameEntries(message string, n int) []LogEntry {
	out := make([]LogEntry, n)
	for i := range out {
		out[i] = LogEntry{Message: message}
	}
	return out
}

func TestLogPatternMiner_TruncatesLongSamplesAndTemplates(t *testing.T) {
	cases := []struct {
		name      string
		runes     int
		wantRunes int
	}{
		{"one under the limit", maxPatternTextRunes - 1, maxPatternTextRunes - 1},
		{"at the limit", maxPatternTextRunes, maxPatternTextRunes},
		{"one over the limit", maxPatternTextRunes + 1, maxPatternTextRunes + 1},
		{"far over the limit", maxPatternTextRunes * 20, maxPatternTextRunes + 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := mustMineLogPatterns(t, []LogEntry{{Message: strings.Repeat("é", c.runes)}})[0]
			if got := len([]rune(p.sample)); got != c.wantRunes {
				t.Errorf("sample length = %d runes, want %d", got, c.wantRunes)
			}
			if got := len([]rune(p.template())); got != c.wantRunes {
				t.Errorf("template length = %d runes, want %d", got, c.wantRunes)
			}
			if truncated := c.runes > maxPatternTextRunes; truncated != strings.HasSuffix(p.template(), "…") {
				t.Errorf("template ends in an ellipsis = %t, want %t", !truncated, truncated)
			}
		})
	}
}

// Truncation is for display only: templates differing past the cut still count
// apart, even though both render as the same truncated text.
func TestLogPatternMiner_CountsTemplatesDifferingPastTheCutApart(t *testing.T) {
	prefix := strings.Repeat("k ", maxPatternTextRunes)
	lines := append(sameEntries(prefix+"alpha", 3), sameEntries(prefix+"gamma delta", 2)...)
	patterns := mustMineLogPatterns(t, lines)
	if len(patterns) != 2 {
		t.Fatalf("patterns = %d, want 2", len(patterns))
	}
	if patterns[0].template() != patterns[1].template() {
		t.Fatalf("templates differ, want both cut to the shared prefix")
	}
	for i, want := range []int{3, 2} {
		if got := patterns[i].stats(0).Count; got != want {
			t.Errorf("pattern %d count = %d, want %d", i, got, want)
		}
	}
}
