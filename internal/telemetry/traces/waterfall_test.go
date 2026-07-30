package traces

import (
	"math"
	"strings"
	"testing"
	"time"
)

var waterfallTestObservedAt = time.Date(2026, 7, 15, 0, 1, 0, 0, time.UTC)

// Deterministic input so tests never depend on the wall clock.
func waterfallTestInput(spans []TraceDetailsSpan, limit int, selectedID string) waterfallBuildInput {
	start := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	end := start.Add(15 * time.Minute)
	return waterfallBuildInput{
		traceID:      "t",
		spans:        spans,
		fetchedSpans: len(spans),
		limit:        limit,
		selectedID:   selectedID,
		requested:    evidenceWindow(start, end),
		effective:    evidenceWindow(start, end),
		observedAt:   waterfallTestObservedAt,
	}
}

func hasWarning(warnings []string, substring string) bool {
	for _, warning := range warnings {
		if strings.Contains(warning, substring) {
			return true
		}
	}
	return false
}

func TestBuildTraceWaterfallOverlappingChildrenUseIntervalUnion(t *testing.T) {
	spans := []TraceDetailsSpan{
		{TraceID: "t", SpanID: "root", SpanName: "root", ServiceName: "api", Timestamp: "2026-07-15T00:00:00Z", Duration: 100_000_000},
		{TraceID: "t", SpanID: "a", ParentSpanID: "root", SpanName: "a", ServiceName: "db", Timestamp: "2026-07-15T00:00:00.010Z", Duration: 50_000_000},
		{TraceID: "t", SpanID: "b", ParentSpanID: "root", SpanName: "b", ServiceName: "cache", Timestamp: "2026-07-15T00:00:00.040Z", Duration: 50_000_000},
	}
	r := buildTraceWaterfall(waterfallTestInput(spans, 500, ""))
	var root WaterfallSpan
	for _, s := range r.Data.Spans {
		if s.SpanID == "root" {
			root = s
		}
	}
	// Child union is [10,90] ms, not the naive 100 ms sum.
	if math.Abs(root.SelfTimeMs-20) > 0.001 {
		t.Fatalf("self_time_ms=%v", root.SelfTimeMs)
	}
	if r.Data.Summary.DurationMs != 100 || r.Data.Summary.MaxDepth != 1 {
		t.Fatalf("summary=%+v", r.Data.Summary)
	}
	if r.Interpretation.EvidenceQuality != evidenceQualityHigh {
		t.Fatalf("evidence_quality=%q", r.Interpretation.EvidenceQuality)
	}
	// Trace-scoped: the trace ID is the scope. No service or environment is invented.
	if r.Request.Scope.TraceID != "t" || r.Request.Scope.Environment != "" {
		t.Fatalf("scope=%+v", r.Request.Scope)
	}
}

func TestBuildTraceWaterfallMissingParentAndSelectedDetails(t *testing.T) {
	spans := []TraceDetailsSpan{{TraceID: "t", SpanID: "orphan", ParentSpanID: "missing", ServiceName: "api", SpanName: "work", Timestamp: "2026-07-15T00:00:00Z", Duration: 10_000_000, SpanAttributes: map[string]interface{}{"key": "value"}}}
	r := buildTraceWaterfall(waterfallTestInput(spans, 1, "orphan"))
	if len(r.Data.Summary.RootSpanIDs) != 1 || r.Data.Summary.RootSpanIDs[0] != "orphan" {
		t.Fatalf("roots=%v", r.Data.Summary.RootSpanIDs)
	}
	if !hasWarning(r.Evidence.Warnings, "missing parent") {
		t.Fatalf("warnings=%v", r.Evidence.Warnings)
	}
	if r.Data.SelectedSpan == nil || r.Data.SelectedSpan.SpanAttributes["key"] != "value" {
		t.Fatalf("selected=%+v", r.Data.SelectedSpan)
	}
	if !r.Evidence.Truncated {
		t.Fatal("limit-sized result must disclose possible truncation")
	}
}

func TestUnionDurationDisjointAndOverlapping(t *testing.T) {
	got := unionDuration([]spanInterval{{0, 10}, {5, 20}, {30, 40}})
	if got != 30 {
		t.Fatalf("union=%d", got)
	}
}

func TestBuildTraceWaterfallDetectsCycle(t *testing.T) {
	spans := []TraceDetailsSpan{
		{TraceID: "t", SpanID: "a", ParentSpanID: "b", Timestamp: "2026-07-15T00:00:00Z", Duration: 10_000_000},
		{TraceID: "t", SpanID: "b", ParentSpanID: "a", Timestamp: "2026-07-15T00:00:00Z", Duration: 10_000_000},
	}
	r := buildTraceWaterfall(waterfallTestInput(spans, 500, ""))
	if !hasWarning(r.Evidence.Warnings, "cycle detected") {
		t.Fatalf("warnings=%v", r.Evidence.Warnings)
	}
}

// Judged pre-env-filter, or a filtered result claims completeness while parent
// self-times absorb the missing children.
func TestBuildTraceWaterfallTruncationJudgedBeforeEnvironmentFilter(t *testing.T) {
	spans := []TraceDetailsSpan{
		{TraceID: "t", SpanID: "root", ServiceName: "api", Timestamp: "2026-07-15T00:00:00Z", Duration: 100_000_000},
	}
	in := waterfallTestInput(spans, 2, "")
	in.fetchedSpans = 2 // API returned exactly the limit; env filtering dropped one.
	r := buildTraceWaterfall(in)
	if !r.Evidence.Truncated {
		t.Fatal("truncation must be judged against the pre-filter fetched count")
	}
	if !hasWarning(r.Evidence.Warnings, "self_time_ms may be overstated") {
		t.Fatalf("truncated result must warn about inflated self time: %v", r.Evidence.Warnings)
	}
	if r.Interpretation.EvidenceQuality != evidenceQualityMedium {
		t.Fatalf("evidence_quality=%q", r.Interpretation.EvidenceQuality)
	}
}

// Instant 0 used to yield a bogus start_offset_ms, trace duration and self time.
func TestBuildTraceWaterfallExcludesUnparseableTimestamps(t *testing.T) {
	spans := []TraceDetailsSpan{
		{TraceID: "t", SpanID: "ok", ServiceName: "api", Timestamp: "2026-07-15T00:00:00Z", Duration: 50_000_000},
		{TraceID: "t", SpanID: "bad", ParentSpanID: "ok", ServiceName: "api", Timestamp: "not-a-timestamp", Duration: 100_000_000},
	}
	r := buildTraceWaterfall(waterfallTestInput(spans, 500, ""))
	if len(r.Data.Spans) != 1 || r.Data.Spans[0].SpanID != "ok" {
		t.Fatalf("spans=%+v", r.Data.Spans)
	}
	if r.Data.Summary.DurationMs != 50 {
		t.Fatalf("duration_ms=%v, want the parseable span's duration", r.Data.Summary.DurationMs)
	}
	if r.Data.Spans[0].StartOffsetMs != 0 || r.Data.Spans[0].SelfTimeMs != 50 {
		t.Fatalf("span=%+v", r.Data.Spans[0])
	}
	if !hasWarning(r.Evidence.Warnings, "unparseable timestamp") {
		t.Fatalf("warnings=%v", r.Evidence.Warnings)
	}
	if !r.Evidence.Partial {
		t.Fatal("dropping a span makes the waterfall partial")
	}
}

// An epoch-zero timestamp is a legal instant and must not be mistaken for "unset".
func TestBuildTraceWaterfallAcceptsEpochTimestamps(t *testing.T) {
	spans := []TraceDetailsSpan{
		{TraceID: "t", SpanID: "epoch", ServiceName: "api", Timestamp: "1970-01-01T00:00:00Z", Duration: 5_000_000},
	}
	r := buildTraceWaterfall(waterfallTestInput(spans, 500, ""))
	if r.Data.Summary.DurationMs != 5 {
		t.Fatalf("duration_ms=%v", r.Data.Summary.DurationMs)
	}
	if r.Data.Summary.Start != "1970-01-01T00:00:00Z" {
		t.Fatalf("start=%q", r.Data.Summary.Start)
	}
}

// Nothing found is not a high-confidence observation.
func TestBuildTraceWaterfallEmptyResultIsInsufficientEvidence(t *testing.T) {
	r := buildTraceWaterfall(waterfallTestInput(nil, 500, ""))
	if r.Interpretation.EvidenceQuality != evidenceQualityInsufficient {
		t.Fatalf("evidence_quality=%q", r.Interpretation.EvidenceQuality)
	}
	if r.Interpretation.ClaimType != traceWaterfallClaimObservation {
		t.Fatalf("claim_type=%q", r.Interpretation.ClaimType)
	}
	if !hasWarning(r.Evidence.Warnings, "no spans found") {
		t.Fatalf("warnings=%v", r.Evidence.Warnings)
	}
	if r.Data.Spans == nil || r.Data.Summary.RootSpanIDs == nil {
		t.Fatal("empty collections must marshal as [] not null")
	}
}

// Identical input must yield identical warnings, or content hashes are meaningless.
func TestBuildTraceWaterfallWarningOrderIsDeterministic(t *testing.T) {
	spans := []TraceDetailsSpan{
		{TraceID: "t", SpanID: "a", ParentSpanID: "gone-1", ServiceName: "api", Timestamp: "2026-07-15T00:00:00Z", Duration: 10_000_000},
		{TraceID: "t", SpanID: "b", ParentSpanID: "gone-2", ServiceName: "api", Timestamp: "2026-07-15T00:00:00Z", Duration: 10_000_000},
		{TraceID: "t", SpanID: "c", ParentSpanID: "gone-3", ServiceName: "api", Timestamp: "2026-07-15T00:00:00Z", Duration: 10_000_000},
		{TraceID: "t", SpanID: "d", ParentSpanID: "gone-4", ServiceName: "api", Timestamp: "2026-07-15T00:00:00Z", Duration: 10_000_000},
		{TraceID: "t", SpanID: "", ServiceName: "api", Timestamp: "2026-07-15T00:00:00Z", Duration: 10_000_000},
		{TraceID: "t", SpanID: "e", ServiceName: "api", Timestamp: "bad", Duration: 10_000_000},
	}
	want := buildTraceWaterfall(waterfallTestInput(spans, 500, "")).Evidence.Warnings
	for i := 0; i < 50; i++ {
		got := buildTraceWaterfall(waterfallTestInput(spans, 500, "")).Evidence.Warnings
		if len(got) != len(want) {
			t.Fatalf("warning count changed: %v vs %v", got, want)
		}
		for j := range got {
			if got[j] != want[j] {
				t.Fatalf("warning order changed on run %d: %v vs %v", i, got, want)
			}
		}
	}
}

// An absent ServiceName is not a service.
func TestBuildTraceWaterfallIgnoresEmptyServiceInCount(t *testing.T) {
	spans := []TraceDetailsSpan{
		{TraceID: "t", SpanID: "a", ServiceName: "api", Timestamp: "2026-07-15T00:00:00Z", Duration: 10_000_000},
		{TraceID: "t", SpanID: "b", ServiceName: "", Timestamp: "2026-07-15T00:00:00Z", Duration: 10_000_000},
	}
	r := buildTraceWaterfall(waterfallTestInput(spans, 500, ""))
	if r.Data.Summary.ServiceCount != 1 {
		t.Fatalf("service_count=%d, want 1", r.Data.Summary.ServiceCount)
	}
}

func TestParseTraceTimestampNano(t *testing.T) {
	cases := []struct {
		in   string
		want int64
		ok   bool
	}{
		{"2026-07-15T00:00:00Z", time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC).UnixNano(), true},
		{"2026-07-15T00:00:00.123456789Z", time.Date(2026, 7, 15, 0, 0, 0, 123456789, time.UTC).UnixNano(), true},
		{"2026-07-15 00:00:00.123456789", time.Date(2026, 7, 15, 0, 0, 0, 123456789, time.UTC).UnixNano(), true},
		{"2026-07-15T00:00:00.123456789", time.Date(2026, 7, 15, 0, 0, 0, 123456789, time.UTC).UnixNano(), true},
		{"", 0, false},
		{"not-a-timestamp", 0, false},
	}
	for _, c := range cases {
		got, ok := parseTraceTimestampNano(c.in)
		if ok != c.ok || got != c.want {
			t.Fatalf("parseTraceTimestampNano(%q)=(%d,%v), want (%d,%v)", c.in, got, ok, c.want, c.ok)
		}
	}
	// Nanosecond precision must survive; the seconds helper delegates to the same parser.
	if got := parseTimestampToUnix("2026-07-15 00:00:00.123456789"); got != time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC).Unix() {
		t.Fatalf("parseTimestampToUnix=%d", got)
	}
}
