package traces

import (
	"math"
	"strings"
	"testing"
)

func TestBuildTraceWaterfallOverlappingChildrenUseIntervalUnion(t *testing.T) {
	spans := []TraceDetailsSpan{
		{TraceID: "t", SpanID: "root", SpanName: "root", ServiceName: "api", Timestamp: "2026-07-15T00:00:00Z", Duration: 100_000_000},
		{TraceID: "t", SpanID: "a", ParentSpanID: "root", SpanName: "a", ServiceName: "db", Timestamp: "2026-07-15T00:00:00.010Z", Duration: 50_000_000},
		{TraceID: "t", SpanID: "b", ParentSpanID: "root", SpanName: "b", ServiceName: "cache", Timestamp: "2026-07-15T00:00:00.040Z", Duration: 50_000_000},
	}
	r := buildTraceWaterfall("t", spans, 500, "")
	var root WaterfallSpan
	for _, s := range r.Spans {
		if s.SpanID == "root" {
			root = s
		}
	}
	// Child union is [10,90] ms, not the naive 100 ms sum.
	if math.Abs(root.SelfTimeMs-20) > 0.001 {
		t.Fatalf("self_time_ms=%v", root.SelfTimeMs)
	}
	if r.Summary.DurationMs != 100 || r.Summary.MaxDepth != 1 {
		t.Fatalf("summary=%+v", r.Summary)
	}
}

func TestBuildTraceWaterfallMissingParentAndSelectedDetails(t *testing.T) {
	spans := []TraceDetailsSpan{{TraceID: "t", SpanID: "orphan", ParentSpanID: "missing", ServiceName: "api", SpanName: "work", Timestamp: "2026-07-15T00:00:00Z", Duration: 10_000_000, SpanAttributes: map[string]interface{}{"key": "value"}}}
	r := buildTraceWaterfall("t", spans, 1, "orphan")
	if len(r.Summary.RootSpanIDs) != 1 || r.Summary.RootSpanIDs[0] != "orphan" {
		t.Fatalf("roots=%v", r.Summary.RootSpanIDs)
	}
	if len(r.Evidence.Warnings) == 0 {
		t.Fatal("expected missing-parent warning")
	}
	if r.SelectedSpan == nil || r.SelectedSpan.SpanAttributes["key"] != "value" {
		t.Fatalf("selected=%+v", r.SelectedSpan)
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
	r := buildTraceWaterfall("t", spans, 500, "")
	found := false
	for _, warning := range r.Evidence.Warnings {
		if strings.Contains(warning, "cycle detected") {
			found = true
		}
	}
	if !found {
		t.Fatalf("warnings=%v", r.Evidence.Warnings)
	}
}
