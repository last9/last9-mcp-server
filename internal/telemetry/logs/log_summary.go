package logs

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	logResponseModeSummary = "summary"
	logResponseModeRaw     = "raw"

	// Results at or under this many lines are cheaper to read raw than summarized.
	maxRawLinesInSummaryMode = 20
	maxSummaryChars          = 4000
	maxSummaryPatterns       = 8
	maxSummaryPatternSamples = 2
	maxSummaryTextRunes      = 160
	summaryHistogramBuckets  = 10
	// Severity text is free-form in OTel; six labels fit its standard levels.
	maxSummarySeverities    = 6
	maxSummarySeverityRunes = 32
)

type logSummary struct {
	LinesSummarized int                      `json:"lines_summarized"`
	SeverityCounts  map[string]int           `json:"severity_counts"`
	OtherSeverities *logSummarySeverityOther `json:"other_severities,omitempty"`
	FirstTimestamp  string                   `json:"first_timestamp,omitempty"`
	LastTimestamp   string                   `json:"last_timestamp,omitempty"`
	Histogram       logSummaryHistogram      `json:"histogram"`
	Patterns        []logSummaryPattern      `json:"patterns"`
	Other           *logSummaryOther         `json:"other,omitempty"`
	DroppedFields   []string                 `json:"dropped_fields,omitempty"`
	Hint            string                   `json:"hint"`
}

type logSummarySeverityOther struct {
	Labels int `json:"labels"`
	Lines  int `json:"lines"`
}

type logSummaryHistogram struct {
	Start         string `json:"start"`
	BucketSeconds int64  `json:"bucket_seconds"`
	Counts        []int  `json:"counts"`
}

type logSummaryPattern struct {
	Template string   `json:"template"`
	Count    int      `json:"count"`
	Samples  []string `json:"samples"`
}

type logSummaryOther struct {
	Patterns int `json:"patterns"`
	Lines    int `json:"lines"`
}

const logSummaryHint = `Summary of the returned lines. Pass response_mode "raw" for the lines themselves; narrow the filter to drill into one pattern.`

func validateLogResponseMode(mode string) error {
	switch mode {
	case "", logResponseModeSummary, logResponseModeRaw:
		return nil
	default:
		return fmt.Errorf("response_mode must be %q or %q, got %q", logResponseModeSummary, logResponseModeRaw, mode)
	}
}

func summarizeLogResult(result map[string]interface{}, startMs, endMs int64) map[string]interface{} {
	kind, items, err := extractResultItems(result)
	if err != nil || kind != "streams" || countLogEntriesInResultItems(items) <= maxRawLinesInSummaryMode {
		return result
	}
	entries := parseServiceLogEntries(map[string]any{"data": result["data"]}, "")
	if len(entries) <= maxRawLinesInSummaryMode {
		return result
	}
	patterns, err := mineLogPatterns(entries)
	if err != nil {
		return result
	}

	summarized := make(map[string]interface{}, len(result))
	for key, value := range result {
		if key != "data" {
			summarized[key] = value
		}
	}
	base := newLogSummary(entries, startMs, endMs)
	for topN, samples := maxSummaryPatterns, maxSummaryPatternSamples; ; {
		summary := base.withPatterns(patterns, topN, samples)
		summarized["summary"] = summary
		if len(formatJSON(summarized)) <= maxSummaryChars {
			return summarized
		}
		if topN == 1 && samples == 0 {
			return dropLargestMetadata(summarized, summary)
		}
		if samples > 1 || topN == 1 {
			samples--
		} else {
			topN--
		}
	}
}

// dropLargestMetadata removes retained top-level keys, largest first, until
// the result fits; the summary names each one so the caller can ask for raw.
func dropLargestMetadata(summarized map[string]interface{}, summary logSummary) map[string]interface{} {
	keys := slices.DeleteFunc(slices.Collect(maps.Keys(summarized)), func(key string) bool { return key == "summary" })
	size := func(key string) int { return len(formatJSON(summarized[key])) }
	slices.SortFunc(keys, func(a, b string) int { return cmp.Or(cmp.Compare(size(b), size(a)), strings.Compare(a, b)) })
	for _, key := range keys {
		if len(formatJSON(summarized)) <= maxSummaryChars {
			break
		}
		delete(summarized, key)
		summary.DroppedFields = append(summary.DroppedFields, key)
		summarized["summary"] = summary
	}
	return summarized
}

func newLogSummary(entries []LogEntry, startMs, endMs int64) logSummary {
	summary := logSummary{
		LinesSummarized: len(entries),
		Histogram:       newLogSummaryHistogram(startMs, endMs),
		Hint:            logSummaryHint,
	}
	severityCounts := map[string]int{}
	var first, last time.Time
	for _, entry := range entries {
		severity := strings.TrimSpace(entry.Severity)
		if severity == "" {
			severity = unknownPatternSeverity
		}
		severityCounts[truncateRunes(severity, maxSummarySeverityRunes)]++

		ts, err := time.Parse(time.RFC3339Nano, entry.Timestamp)
		if err != nil {
			continue
		}
		if first.IsZero() || ts.Before(first) {
			first = ts
		}
		if ts.After(last) {
			last = ts
		}
		summary.Histogram.record(ts, startMs)
	}
	if !first.IsZero() {
		summary.FirstTimestamp = first.UTC().Format(time.RFC3339)
		summary.LastTimestamp = last.UTC().Format(time.RFC3339)
	}
	summary.SeverityCounts, summary.OtherSeverities = boundSeverityCounts(severityCounts)
	return summary
}

func boundSeverityCounts(counts map[string]int) (map[string]int, *logSummarySeverityOther) {
	if len(counts) <= maxSummarySeverities {
		return counts, nil
	}
	labels := slices.Collect(maps.Keys(counts))
	slices.SortFunc(labels, func(a, b string) int { return cmp.Or(cmp.Compare(counts[b], counts[a]), strings.Compare(a, b)) })
	other := &logSummarySeverityOther{Labels: len(labels) - maxSummarySeverities}
	for _, label := range labels[maxSummarySeverities:] {
		other.Lines += counts[label]
		delete(counts, label)
	}
	return counts, other
}

func newLogSummaryHistogram(startMs, endMs int64) logSummaryHistogram {
	bucketSeconds := max((endMs-startMs)/1000/summaryHistogramBuckets, 1)
	return logSummaryHistogram{
		Start:         time.UnixMilli(startMs).UTC().Format(time.RFC3339),
		BucketSeconds: bucketSeconds,
		Counts:        make([]int, summaryHistogramBuckets),
	}
}

// record clamps into the edge buckets: timestamps are second-resolution and
// the window's end is inclusive, so a line can sit just past the last bucket.
func (h *logSummaryHistogram) record(ts time.Time, startMs int64) {
	bucket := (ts.UnixMilli() - startMs) / 1000 / h.BucketSeconds
	h.Counts[min(max(bucket, 0), int64(len(h.Counts)-1))]++
}

func (s logSummary) withPatterns(patterns []*logPattern, topN, samples int) logSummary {
	ranked := slices.Clone(patterns)
	top, rest := rankLogPatterns(ranked, topN, (*logPattern).template, func(a, b *logPattern) int {
		return cmp.Compare(b.windows[0].count, a.windows[0].count)
	})
	s.Patterns = make([]logSummaryPattern, 0, len(top))
	for _, p := range top {
		s.Patterns = append(s.Patterns, logSummaryPattern{
			Template: truncateRunes(p.template(), maxSummaryTextRunes),
			Count:    p.windows[0].count,
			Samples:  summarySamples(p.samples, samples),
		})
	}
	if len(rest) > 0 {
		s.Other = &logSummaryOther{Patterns: len(rest)}
		for _, p := range rest {
			s.Other.Lines += p.windows[0].count
		}
	}
	return s
}

func summarySamples(samples []string, n int) []string {
	out := make([]string, 0, n)
	for _, sample := range samples[:min(n, len(samples))] {
		out = append(out, truncateRunes(sample, maxSummaryTextRunes))
	}
	return out
}

func truncateRunes(text string, limit int) string {
	if utf8.RuneCountInString(text) <= limit {
		return text
	}
	return string([]rune(text)[:limit-1]) + "…"
}
