package traces

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	jsonschema "github.com/google/jsonschema-go/jsonschema"
)

const evidenceSchemaPath = "../../../contracts/investigation-evidence-v1.schema.json"

func resolvedEvidenceSchema(t *testing.T) *jsonschema.Resolved {
	t.Helper()
	b, err := os.ReadFile(filepath.Clean(evidenceSchemaPath))
	if err != nil {
		t.Fatal(err)
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal(b, &schema); err != nil {
		t.Fatal(err)
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		t.Fatalf("resolve evidence schema: %v", err)
	}
	return resolved
}

const waterfallFixturePath = "../../../contracts/fixtures/evidence-trace-waterfall.json"

// Canonical input behind the committed fixture. Generating it from the producer is
// what stops a hand-written fixture holding combinations this code never emits.
func waterfallFixtureInput() waterfallBuildInput {
	start := time.Date(2026, 7, 15, 8, 30, 0, 0, time.UTC)
	return waterfallBuildInput{
		traceID:     "seed-trace-confirming-01",
		environment: "production",
		spans: []TraceDetailsSpan{
			{TraceID: "seed-trace-confirming-01", SpanID: "root", SpanName: "POST /checkout", SpanKind: "SPAN_KIND_SERVER", ServiceName: "checkout", Timestamp: "2026-07-15T08:30:00Z", Duration: 1_450_000_000},
			{TraceID: "seed-trace-confirming-01", SpanID: "pricing", ParentSpanID: "root", SpanName: "GET /price", SpanKind: "SPAN_KIND_CLIENT", ServiceName: "pricing-service", Timestamp: "2026-07-15T08:30:00.120Z", Duration: 820_000_000, StatusCode: traceWaterfallErrorStatusCode},
			{TraceID: "seed-trace-confirming-01", SpanID: "audit", ParentSpanID: "root", SpanName: "publish audit", SpanKind: "SPAN_KIND_PRODUCER", ServiceName: "checkout", Timestamp: "2026-07-15T08:30:00.980Z", Duration: 80_000_000},
		},
		fetchedSpans: 3,
		limit:        500,
		requested:    evidenceWindow(start, start.Add(15*time.Minute)),
		effective:    evidenceWindow(start, start.Add(15*time.Minute)),
		observedAt:   time.Date(2026, 7, 15, 8, 41, 5, 0, time.UTC),
	}
}

// Run with LAST9_UPDATE_FIXTURES=1 to regenerate, then refresh the content_sha256
// entries in contracts/fixtures/workflow-cases-v1.json.
func TestWaterfallFixtureMatchesProducerOutput(t *testing.T) {
	canonical, err := marshalSanitizedTraceWaterfall(buildTraceWaterfall(waterfallFixtureInput()))
	if err != nil {
		t.Fatal(err)
	}
	var indented bytes.Buffer
	if err := json.Indent(&indented, canonical, "", "  "); err != nil {
		t.Fatal(err)
	}
	generated := indented.Bytes()
	generated = append(generated, '\n')
	path := filepath.Clean(waterfallFixturePath)
	if os.Getenv("LAST9_UPDATE_FIXTURES") == "1" {
		if err := os.WriteFile(path, generated, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Log("fixture regenerated; refresh workflow-cases-v1.json content_sha256")
		return
	}
	committed, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(committed) != string(generated) {
		t.Fatalf("%s has drifted from producer output; regenerate with LAST9_UPDATE_FIXTURES=1", waterfallFixturePath)
	}
}

// Validates *marshalled* output. contracts_test.go only checked hand-written fixtures,
// so a non-conforming producer could stamp v1 and keep CI green.
func TestTraceWaterfallOutputSatisfiesEvidenceContract(t *testing.T) {
	schema := resolvedEvidenceSchema(t)
	start := time.Date(2026, 7, 15, 8, 30, 0, 0, time.UTC)
	end := start.Add(15 * time.Minute)

	cases := map[string]waterfallBuildInput{
		"populated": {
			traceID:     "trace-under-test",
			environment: "production",
			spans: []TraceDetailsSpan{
				{TraceID: "trace-under-test", SpanID: "root", SpanName: "POST /checkout", SpanKind: "SPAN_KIND_SERVER", ServiceName: "checkout", Timestamp: "2026-07-15T08:30:00Z", Duration: 1_450_000_000},
				{TraceID: "trace-under-test", SpanID: "pricing", ParentSpanID: "root", SpanName: "GET /price", ServiceName: "pricing-service", Timestamp: "2026-07-15T08:30:00.120Z", Duration: 820_000_000, StatusCode: traceWaterfallErrorStatusCode},
			},
			fetchedSpans: 2,
			limit:        500,
			selectedID:   "pricing",
			requested:    evidenceWindow(start, end),
			effective:    evidenceWindow(start, end),
			observedAt:   start.Add(11 * time.Minute),
		},
		// Degraded paths must conform too; they are the likely ones during an incident.
		"empty": {
			traceID:      "missing-trace",
			spans:        nil,
			fetchedSpans: 0,
			limit:        500,
			requested:    evidenceWindow(start, end),
			effective:    evidenceWindow(start, end),
			observedAt:   start,
		},
		"truncated-and-degraded": {
			traceID: "degraded-trace",
			spans: []TraceDetailsSpan{
				{TraceID: "degraded-trace", SpanID: "orphan", ParentSpanID: "absent", ServiceName: "checkout", Timestamp: "2026-07-15T08:30:00Z", Duration: 10_000_000},
				{TraceID: "degraded-trace", SpanID: "unparseable", ServiceName: "checkout", Timestamp: "nonsense", Duration: 10_000_000},
			},
			fetchedSpans: 2,
			limit:        2,
			requested:    evidenceWindow(start, end),
			effective:    evidenceWindow(start, end),
			observedAt:   start,
		},
	}

	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			marshalled, err := marshalSanitizedTraceWaterfall(buildTraceWaterfall(in))
			if err != nil {
				t.Fatal(err)
			}
			var payload map[string]any
			if err := json.Unmarshal(marshalled, &payload); err != nil {
				t.Fatal(err)
			}
			if err := schema.Validate(payload); err != nil {
				t.Fatalf("tool output does not satisfy %s: %v", evidenceSchemaPath, err)
			}
			if payload["contract_version"] != investigationEvidenceVersion {
				t.Fatalf("contract_version=%v", payload["contract_version"])
			}
			if payload["analysis_version"] != traceWaterfallAnalysisVersion {
				t.Fatalf("analysis_version=%v", payload["analysis_version"])
			}
			interpretation, ok := payload["interpretation"].(map[string]any)
			if !ok {
				t.Fatal("interpretation must be an object")
			}
			// contracts/README.md forbids causal claims from this producer.
			if interpretation["claim_type"] == "cause" {
				t.Fatal("causal claim type is forbidden")
			}
		})
	}
}

// Identical input must marshal identically, or evidence content hashes are meaningless.
func TestTraceWaterfallOutputIsByteStable(t *testing.T) {
	start := time.Date(2026, 7, 15, 8, 30, 0, 0, time.UTC)
	in := waterfallBuildInput{
		traceID: "stable-trace",
		spans: []TraceDetailsSpan{
			{TraceID: "stable-trace", SpanID: "a", ParentSpanID: "gone-1", ServiceName: "api", Timestamp: "2026-07-15T08:30:00Z", Duration: 10_000_000},
			{TraceID: "stable-trace", SpanID: "b", ParentSpanID: "gone-2", ServiceName: "api", Timestamp: "2026-07-15T08:30:00Z", Duration: 10_000_000},
			{TraceID: "stable-trace", SpanID: "c", ParentSpanID: "gone-3", ServiceName: "api", Timestamp: "2026-07-15T08:30:00Z", Duration: 10_000_000},
			{TraceID: "stable-trace", SpanID: "d", ServiceName: "api", Timestamp: "bad", Duration: 10_000_000},
		},
		fetchedSpans: 4,
		limit:        500,
		requested:    evidenceWindow(start, start.Add(15*time.Minute)),
		effective:    evidenceWindow(start, start.Add(15*time.Minute)),
		observedAt:   start,
	}
	first, err := marshalSanitizedTraceWaterfall(buildTraceWaterfall(in))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 50; i++ {
		next, err := marshalSanitizedTraceWaterfall(buildTraceWaterfall(in))
		if err != nil {
			t.Fatal(err)
		}
		if string(next) != string(first) {
			t.Fatalf("run %d produced different bytes:\n%s\n%s", i, first, next)
		}
	}
}
