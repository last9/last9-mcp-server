package logs

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	summaryTestStartMs = int64(1_700_000_000_000)
	summaryTestEndMs   = summaryTestStartMs + 10*60*1000
)

type summaryTestLine struct {
	severity string
	offset   time.Duration
	message  string
}

func streamsResult(lines []summaryTestLine) map[string]interface{} {
	bySeverity := map[string][]interface{}{}
	var order []string
	for _, line := range lines {
		if _, seen := bySeverity[line.severity]; !seen {
			order = append(order, line.severity)
		}
		ts := time.UnixMilli(summaryTestStartMs).Add(line.offset).UnixNano()
		bySeverity[line.severity] = append(bySeverity[line.severity], []interface{}{strconv.FormatInt(ts, 10), line.message})
	}
	streams := make([]interface{}, 0, len(order))
	for _, severity := range order {
		streams = append(streams, map[string]interface{}{
			"stream": map[string]interface{}{"severity": severity},
			"values": bySeverity[severity],
		})
	}
	return map[string]interface{}{
		"data": map[string]interface{}{"resultType": "streams", "result": streams},
	}
}

func repeatedLines(n int, severity string, message func(i int) string) []summaryTestLine {
	lines := make([]summaryTestLine, n)
	for i := range lines {
		lines[i] = summaryTestLine{severity: severity, offset: time.Duration(i) * time.Second, message: message(i)}
	}
	return lines
}

func decodeSummary(t *testing.T, result map[string]interface{}) logSummary {
	t.Helper()
	raw, err := json.Marshal(result["summary"])
	if err != nil {
		t.Fatalf("marshal summary: %v", err)
	}
	var summary logSummary
	if err := json.Unmarshal(raw, &summary); err != nil {
		t.Fatalf("decode summary %s: %v", raw, err)
	}
	return summary
}

func TestSummarizeLogResult_KeepsResultsAtOrUnderThreshold(t *testing.T) {
	for _, n := range []int{0, 1, maxRawLinesInSummaryMode} {
		result := streamsResult(repeatedLines(n, "ERROR", func(i int) string { return fmt.Sprintf("timeout after %d ms", i) }))
		got := summarizeLogResult(result, summaryTestStartMs, summaryTestEndMs)
		if _, ok := got["data"]; !ok {
			t.Errorf("%d lines: data dropped, want the raw result unchanged", n)
		}
		if _, ok := got["summary"]; ok {
			t.Errorf("%d lines: summarized, want raw", n)
		}
	}
}

func TestSummarizeLogResult_KeepsAggregateResults(t *testing.T) {
	result := map[string]interface{}{
		"data": map[string]interface{}{"resultType": "matrix", "result": make([]interface{}, 100)},
	}
	got := summarizeLogResult(result, summaryTestStartMs, summaryTestEndMs)
	if _, ok := got["summary"]; ok {
		t.Fatal("aggregate result summarized, want it unchanged")
	}
}

func TestSummarizeLogResult_SummarizesPastThreshold(t *testing.T) {
	lines := repeatedLines(maxRawLinesInSummaryMode, "ERROR", func(i int) string {
		return fmt.Sprintf("upstream timeout after %d ms", 100+i)
	})
	lines = append(lines, summaryTestLine{severity: "INFO", offset: 9*time.Minute + 30*time.Second, message: "request served"})

	result := streamsResult(lines)
	result["total_matching_lines"] = 900
	result["l9_guidance"] = map[string]any{"message": "truncated"}
	got := summarizeLogResult(result, summaryTestStartMs, summaryTestEndMs)

	if _, ok := got["data"]; ok {
		t.Error("raw data kept in summary mode")
	}
	if got["total_matching_lines"] != 900 || got["l9_guidance"] == nil {
		t.Errorf("metadata dropped: %#v", got)
	}
	summary := decodeSummary(t, got)
	if summary.LinesSummarized != 21 {
		t.Errorf("lines_summarized = %d, want 21", summary.LinesSummarized)
	}
	if summary.SeverityCounts["ERROR"] != 20 || summary.SeverityCounts["INFO"] != 1 {
		t.Errorf("severity_counts = %v, want ERROR 20, INFO 1", summary.SeverityCounts)
	}
	if summary.FirstTimestamp != "2023-11-14T22:13:20Z" || summary.LastTimestamp != "2023-11-14T22:22:50Z" {
		t.Errorf("first/last = %s/%s", summary.FirstTimestamp, summary.LastTimestamp)
	}
	if summary.Histogram.BucketSeconds != 60 || summary.Histogram.Start != "2023-11-14T22:13:20Z" {
		t.Errorf("histogram = %+v, want 60s buckets from the window start", summary.Histogram)
	}
	wantCounts := []int{20, 0, 0, 0, 0, 0, 0, 0, 0, 1}
	if fmt.Sprint(summary.Histogram.Counts) != fmt.Sprint(wantCounts) {
		t.Errorf("histogram counts = %v, want %v", summary.Histogram.Counts, wantCounts)
	}
	if len(summary.Patterns) != 2 {
		t.Fatalf("patterns = %+v, want 2", summary.Patterns)
	}
	top := summary.Patterns[0]
	if top.Template != "upstream timeout after <*> ms" || top.Count != 20 {
		t.Errorf("top pattern = %+v", top)
	}
	if len(top.Samples) != maxSummaryPatternSamples || top.Samples[0] != "upstream timeout after 100 ms" {
		t.Errorf("samples = %v, want the first %d distinct lines", top.Samples, maxSummaryPatternSamples)
	}
	if summary.Other != nil {
		t.Errorf("other = %+v, want none under top_n", summary.Other)
	}
	if !strings.Contains(summary.Hint, `response_mode "raw"`) {
		t.Errorf("hint %q does not name the raw mode", summary.Hint)
	}
}

func TestSummarizeLogResult_FitsCharBudgetOnWideData(t *testing.T) {
	var lines []summaryTestLine
	for p := range 40 {
		word := strings.Repeat(string(rune('a'+p%26)), 3+p/26)
		lines = append(lines, repeatedLines(5+p, "WARN", func(i int) string {
			return fmt.Sprintf("%s handler failed request %d: %s", word, i, strings.Repeat(word+" detail ", 60))
		})...)
	}

	got := summarizeLogResult(streamsResult(lines), summaryTestStartMs, summaryTestEndMs)
	if size := len(formatJSON(got)); size > maxSummaryChars {
		t.Fatalf("summary is %d chars, want <= %d", size, maxSummaryChars)
	}
	summary := decodeSummary(t, got)
	if len(summary.Patterns) == 0 || summary.Other == nil {
		t.Fatalf("patterns = %d, other = %+v; want top patterns plus an other bucket", len(summary.Patterns), summary.Other)
	}
	total := summary.Other.Lines
	for _, p := range summary.Patterns {
		total += p.Count
		for _, sample := range p.Samples {
			if n := len([]rune(sample)); n > maxSummaryTextRunes {
				t.Errorf("sample is %d runes, want <= %d", n, maxSummaryTextRunes)
			}
		}
	}
	if total != len(lines) || summary.Other.Patterns+len(summary.Patterns) != 40 {
		t.Errorf("patterns+other cover %d lines in %d patterns, want %d in 40", total, summary.Other.Patterns+len(summary.Patterns), len(lines))
	}
	if summary.Patterns[0].Count != 44 {
		t.Errorf("top pattern count = %d, want the busiest (44)", summary.Patterns[0].Count)
	}
}

func TestValidateLogResponseMode(t *testing.T) {
	for _, mode := range []string{"", "summary", "raw"} {
		if err := validateLogResponseMode(mode); err != nil {
			t.Errorf("%q: %v", mode, err)
		}
	}
	for _, mode := range []string{"RAW", "lines", " raw"} {
		err := validateLogResponseMode(mode)
		if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("got %q", mode)) {
			t.Errorf("%q: err = %v, want it rejected and quoted", mode, err)
		}
	}
}
