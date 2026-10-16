package logs

import (
	"fmt"
	"strings"
	"testing"
)

func minePatterns(windows int, entries ...[]LogEntry) []*logPattern {
	miner := newLogPatternMiner(windows)
	for window, list := range entries {
		for _, entry := range list {
			miner.add(window, entry)
		}
	}
	return miner.patterns
}

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

	got := templates(minePatterns(1, lines))
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

	got := templates(minePatterns(1, lines))
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

	got := templates(minePatterns(1, lines))
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

	got := templates(minePatterns(1, lines))
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

	patterns := minePatterns(2, baseline, current)
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
	got := templates(minePatterns(1, []LogEntry{{Message: ""}, {Message: "   "}}))
	if len(got) != 1 || got[0] != emptyPatternTemplate {
		t.Fatalf("templates = %q, want one %q pattern", got, emptyPatternTemplate)
	}
}

func TestLogPatternMiner_TruncatesLongSamples(t *testing.T) {
	long := strings.Repeat("x", maxPatternSampleRunes+50)
	p := minePatterns(1, []LogEntry{{Message: long}})[0]
	if got := len([]rune(p.sample)); got != maxPatternSampleRunes+1 {
		t.Fatalf("sample length = %d runes, want %d plus an ellipsis", got, maxPatternSampleRunes)
	}
}
