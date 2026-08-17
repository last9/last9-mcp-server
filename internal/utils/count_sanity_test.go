package utils

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/models"
)

func countAggregatePipeline(serviceEqValues ...string) []map[string]interface{} {
	andConditions := []interface{}{}
	for _, service := range serviceEqValues {
		andConditions = append(andConditions, map[string]interface{}{
			"$eq": []interface{}{"ServiceName", service},
		})
	}
	andConditions = append(andConditions, map[string]interface{}{
		"$eq": []interface{}{"SeverityText", "ERROR"},
	})

	return []map[string]interface{}{
		{
			"type": "filter",
			"query": map[string]interface{}{
				"$and": andConditions,
			},
		},
		{
			"type": "aggregate",
			"aggregates": []interface{}{
				map[string]interface{}{
					"function": map[string]interface{}{"$count": []interface{}{}},
					"as":       "_count",
				},
			},
		},
	}
}

// aggregateCountResponse builds the real log-API aggregate response shape:
// each row is {"metric": {<as-alias>: <count number>, ...labels as strings},
// "values": []}. Verified against live backend responses.
func aggregateCountResponse(as string, matchedCount any) map[string]interface{} {
	return map[string]interface{}{
		"data": map[string]interface{}{
			"resultType": "matrix",
			"result": []interface{}{
				map[string]interface{}{
					"metric": map[string]interface{}{
						as: matchedCount,
					},
					"values": []interface{}{},
				},
			},
		},
	}
}

func aggregateMixedMetricResponse(metric map[string]any) map[string]interface{} {
	return map[string]interface{}{
		"data": map[string]interface{}{
			"resultType": "matrix",
			"result": []interface{}{
				map[string]interface{}{
					"metric": metric,
					"values": []interface{}{},
				},
			},
		},
	}
}

// oldPromShapeAggregateResponse mirrors the Prometheus instant-query
// "value":[ts,val] shape this guardrail originally (wrongly) assumed for
// aggregate rows. It carries no numeric "metric" field, so it must fail
// closed rather than be miscounted as zero matches.
func oldPromShapeAggregateResponse(matchedCount int) map[string]interface{} {
	return map[string]interface{}{
		"data": map[string]interface{}{
			"resultType": "matrix",
			"result": []interface{}{
				map[string]interface{}{
					"value": []interface{}{float64(1_700_000_000), float64(matchedCount)},
				},
			},
		},
	}
}

func sanityTestCfg(t *testing.T, apiBaseURL string) models.Config {
	t.Helper()
	return models.Config{
		APIBaseURL:         apiBaseURL,
		PrometheusReadURL:  "https://prom.example/read",
		PrometheusUsername: "u",
		PrometheusPassword: "p",
		TokenManager: &auth.TokenManager{
			AccessToken: "test-token",
			ExpiresAt:   time.Now().Add(time.Hour),
		},
	}
}

func promVolumeServer(t *testing.T, volume float64) (*httptest.Server, *int) {
	t.Helper()
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		body, _ := json.Marshal([]map[string]any{
			{"metric": map[string]string{}, "value": []any{1_700_000_000, volume}},
		})
		_, _ = w.Write(body)
	}))
	return srv, &calls
}

func promVolumeServerAssert(t *testing.T, volume float64, assertReq func(t *testing.T, r *http.Request)) (*httptest.Server, *int) {
	t.Helper()
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if assertReq != nil {
			assertReq(t, r)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		body, _ := json.Marshal([]map[string]any{
			{"metric": map[string]string{}, "value": []any{1_700_000_000, volume}},
		})
		_, _ = w.Write(body)
	}))
	return srv, &calls
}

func TestAppendCountSanity_HighRatioAddsNote(t *testing.T) {
	srv, _ := promVolumeServer(t, 1000)
	defer srv.Close()

	cfg := sanityTestCfg(t, srv.URL)
	pipeline := countAggregatePipeline("orders-service")
	response := aggregateCountResponse("_count", float64(750))

	got := AppendCountSanity(context.Background(), srv.Client(), cfg, pipeline, 0, 480*60*1000, response)

	sanity, ok := got["l9_sanity"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected l9_sanity block, got %#v", got)
	}
	if sanity["matched_count"] != int64(750) {
		t.Errorf("matched_count = %v, want 750", sanity["matched_count"])
	}
	if sanity["service_log_volume"] != float64(1000) {
		t.Errorf("service_log_volume = %v, want 1000", sanity["service_log_volume"])
	}
	if sanity["ratio"] != 0.75 {
		t.Errorf("ratio = %v, want 0.75", sanity["ratio"])
	}
	note, _ := sanity["note"].(string)
	if note == "" {
		t.Fatal("expected non-empty note for ratio > 5%")
	}
}

func TestAppendCountSanity_LowRatioEmptyNote(t *testing.T) {
	srv, _ := promVolumeServer(t, 100000)
	defer srv.Close()

	cfg := sanityTestCfg(t, srv.URL)
	pipeline := countAggregatePipeline("orders-service")
	response := aggregateCountResponse("_count", float64(750))

	got := AppendCountSanity(context.Background(), srv.Client(), cfg, pipeline, 0, 480*60*1000, response)

	sanity, ok := got["l9_sanity"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected l9_sanity block, got %#v", got)
	}
	if sanity["matched_count"] != int64(750) {
		t.Errorf("matched_count = %v, want 750", sanity["matched_count"])
	}
	if sanity["service_log_volume"] != float64(100000) {
		t.Errorf("service_log_volume = %v, want 100000", sanity["service_log_volume"])
	}
	if note, _ := sanity["note"].(string); note != "" {
		t.Errorf("note = %q, want empty for ratio <= 5%%", note)
	}
}

func TestAppendCountSanity_NoAggregateStageSkipsUntouched(t *testing.T) {
	srv, calls := promVolumeServer(t, 1000)
	defer srv.Close()

	cfg := sanityTestCfg(t, srv.URL)
	pipeline := []map[string]interface{}{
		{
			"type": "filter",
			"query": map[string]interface{}{
				"$eq": []interface{}{"ServiceName", "orders-service"},
			},
		},
	}
	response := map[string]interface{}{"data": map[string]interface{}{"resultType": "streams", "result": []interface{}{}}}

	got := AppendCountSanity(context.Background(), srv.Client(), cfg, pipeline, 0, 480*60*1000, response)

	if _, ok := got["l9_sanity"]; ok {
		t.Fatal("expected no l9_sanity block when pipeline has no count aggregate")
	}
	if *calls != 0 {
		t.Errorf("expected no prometheus call, got %d", *calls)
	}
}

func TestAppendCountSanity_MultipleServicesSkipsUntouched(t *testing.T) {
	srv, calls := promVolumeServer(t, 1000)
	defer srv.Close()

	cfg := sanityTestCfg(t, srv.URL)
	pipeline := countAggregatePipeline("orders-service", "billing-service")
	response := aggregateCountResponse("_count", float64(750))

	got := AppendCountSanity(context.Background(), srv.Client(), cfg, pipeline, 0, 480*60*1000, response)

	if _, ok := got["l9_sanity"]; ok {
		t.Fatal("expected no l9_sanity block when more than one ServiceName is present")
	}
	if *calls != 0 {
		t.Errorf("expected no prometheus call, got %d", *calls)
	}
}

func TestAppendCountSanity_PromErrorLeavesResponseUntouched(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"boom"}`))
	}))
	defer srv.Close()

	cfg := sanityTestCfg(t, srv.URL)
	pipeline := countAggregatePipeline("orders-service")
	response := aggregateCountResponse("_count", float64(750))

	got := AppendCountSanity(context.Background(), srv.Client(), cfg, pipeline, 0, 480*60*1000, response)

	if _, ok := got["l9_sanity"]; ok {
		t.Fatal("expected no l9_sanity block when the baseline prometheus query fails")
	}
}

func TestAppendCountSanity_OldPromShapeFailsClosed(t *testing.T) {
	srv, calls := promVolumeServer(t, 1000)
	defer srv.Close()

	cfg := sanityTestCfg(t, srv.URL)
	pipeline := countAggregatePipeline("orders-service")
	response := oldPromShapeAggregateResponse(750)

	got := AppendCountSanity(context.Background(), srv.Client(), cfg, pipeline, 0, 480*60*1000, response)

	if _, ok := got["l9_sanity"]; ok {
		t.Fatal("expected no l9_sanity block for a row with no numeric metric field (old, wrong shape)")
	}
	if *calls != 0 {
		t.Errorf("expected no prometheus call when the matched count can't be parsed, got %d", *calls)
	}
}

func TestExtractSingleServiceName_DedupSameService(t *testing.T) {
	pipeline := countAggregatePipeline("orders-service", "orders-service")
	service, ok := ExtractSingleServiceName(pipeline)
	if !ok {
		t.Fatal("expected a single service name to be extracted")
	}
	if service != "orders-service" {
		t.Fatalf("service = %q, want orders-service", service)
	}
}

func TestExtractSingleServiceName_NotNegatedServiceSkips(t *testing.T) {
	pipeline := []map[string]interface{}{
		{
			"type": "filter",
			"query": map[string]interface{}{
				"$and": []interface{}{
					map[string]interface{}{
						"$not": map[string]interface{}{
							"$eq": []interface{}{"ServiceName", "orders-service"},
						},
					},
					map[string]interface{}{
						"$eq": []interface{}{"SeverityText", "ERROR"},
					},
				},
			},
		},
		{
			"type": "aggregate",
			"aggregates": []interface{}{
				map[string]interface{}{
					"function": map[string]interface{}{"$count": []interface{}{}},
					"as":       "_count",
				},
			},
		},
	}

	if _, ok := ExtractSingleServiceName(pipeline); ok {
		t.Fatal("expected negated service to not be treated as a positive pin")
	}
}

func TestAppendCountSanity_MixedAggregateMetricSumsOnlyCountAlias(t *testing.T) {
	srv, _ := promVolumeServer(t, 1000)
	defer srv.Close()

	cfg := sanityTestCfg(t, srv.URL)
	pipeline := countAggregatePipeline("orders-service")
	response := aggregateMixedMetricResponse(map[string]any{
		"_count":   float64(750),
		"avg_dur":  float64(123.5),
		"__ts__":   "1700000000",
		"service":  "orders-service",
		"ignored":  json.Number("999"),
		"also_str": "1",
	})

	got := AppendCountSanity(context.Background(), srv.Client(), cfg, pipeline, 0, 480*60*1000, response)
	sanity := got["l9_sanity"].(map[string]interface{})
	if sanity["matched_count"] != int64(750) {
		t.Errorf("matched_count = %v, want 750", sanity["matched_count"])
	}
}

func TestAppendCountSanity_RatioBoundaryExactly5PctHasEmptyNote(t *testing.T) {
	srv, _ := promVolumeServer(t, 1000)
	defer srv.Close()

	cfg := sanityTestCfg(t, srv.URL)
	pipeline := countAggregatePipeline("orders-service")
	response := aggregateCountResponse("_count", float64(50))

	got := AppendCountSanity(context.Background(), srv.Client(), cfg, pipeline, 0, 480*60*1000, response)
	sanity := got["l9_sanity"].(map[string]interface{})
	if sanity["ratio"] != 0.05 {
		t.Errorf("ratio = %v, want 0.05", sanity["ratio"])
	}
	if note, _ := sanity["note"].(string); note != "" {
		t.Errorf("note = %q, want empty for ratio == 5%%", note)
	}
}

func TestAppendCountSanity_ZeroMatchedNoParseStageSkipsAndNoPromCall(t *testing.T) {
	srv, calls := promVolumeServer(t, 1000)
	defer srv.Close()

	cfg := sanityTestCfg(t, srv.URL)
	pipeline := countAggregatePipeline("orders-service")
	response := aggregateCountResponse("_count", float64(0))

	got := AppendCountSanity(context.Background(), srv.Client(), cfg, pipeline, 0, 480*60*1000, response)
	if _, ok := got["l9_sanity"]; ok {
		t.Fatal("expected no l9_sanity block when matched count is 0")
	}
	if *calls != 0 {
		t.Errorf("expected no prometheus call when matched count is 0, got %d", *calls)
	}
}

func TestAppendCountSanity_PromValueStringShape(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		body, _ := json.Marshal([]map[string]any{
			{"metric": map[string]string{}, "value": []any{"1700000000", "1000"}},
		})
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	cfg := sanityTestCfg(t, srv.URL)
	pipeline := countAggregatePipeline("orders-service")
	response := aggregateCountResponse("_count", float64(750))

	got := AppendCountSanity(context.Background(), srv.Client(), cfg, pipeline, 0, 480*60*1000, response)
	if _, ok := got["l9_sanity"]; !ok {
		t.Fatalf("expected l9_sanity block, got %#v", got)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

// TestAppendCountSanity_ZeroMatchedWithParseStageAddsSanityNote verifies the
// most suspicious outcome — a zero count from a pipeline with a parse stage —
// still gets an l9_sanity block (self-correcting note pointing at
// get_log_attributes_for_pipeline's sample_body), without a PromQL baseline
// fetch since there is nothing to compare a zero against.
func TestAppendCountSanity_ZeroMatchedWithParseStageAddsSanityNote(t *testing.T) {
	srv, calls := promVolumeServer(t, 1000)
	defer srv.Close()

	cfg := sanityTestCfg(t, srv.URL)
	pipeline := []map[string]interface{}{
		{
			"type": "filter",
			"query": map[string]interface{}{
				"$eq": []interface{}{"ServiceName", "orders-service"},
			},
		},
		{
			"type":   "parse",
			"parser": "json",
			"field":  "Body",
		},
		{
			"type": "aggregate",
			"aggregates": []interface{}{
				map[string]interface{}{
					"function": map[string]interface{}{"$count": []interface{}{}},
					"as":       "_count",
				},
			},
		},
	}
	response := aggregateCountResponse("_count", float64(0))

	got := AppendCountSanity(context.Background(), srv.Client(), cfg, pipeline, 0, 480*60*1000, response)

	sanity, ok := got["l9_sanity"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected l9_sanity block for zero count with a parse stage, got %#v", got)
	}
	if sanity["matched_count"] != int64(0) {
		t.Errorf("matched_count = %v, want 0", sanity["matched_count"])
	}
	note, _ := sanity["note"].(string)
	if note == "" {
		t.Fatal("expected non-empty note for zero count with a parse stage")
	}
	if *calls != 0 {
		t.Errorf("expected no prometheus baseline call for a zero count, got %d", *calls)
	}
}

// TestAppendCountSanity_ZeroMatchedGroupbyEmptyResultAddsSanityNote verifies
// the empty-result-set shape a groupby'd $count with zero matches actually
// takes (data.result: []), as opposed to a single zero-valued row, still gets
// the same zero-count l9_sanity block when the pipeline has a parse stage.
func TestAppendCountSanity_ZeroMatchedGroupbyEmptyResultAddsSanityNote(t *testing.T) {
	srv, calls := promVolumeServer(t, 1000)
	defer srv.Close()

	cfg := sanityTestCfg(t, srv.URL)
	pipeline := []map[string]interface{}{
		{
			"type": "filter",
			"query": map[string]interface{}{
				"$eq": []interface{}{"ServiceName", "orders-service"},
			},
		},
		{
			"type":   "parse",
			"parser": "json",
			"field":  "Body",
		},
		{
			"type": "aggregate",
			"aggregates": []interface{}{
				map[string]interface{}{
					"function": map[string]interface{}{"$count": []interface{}{}},
					"as":       "_count",
				},
			},
			"groupby": map[string]interface{}{"ServiceName": "service"},
		},
	}
	response := map[string]interface{}{
		"data": map[string]interface{}{
			"resultType": "matrix",
			"result":     []interface{}{},
		},
	}

	got := AppendCountSanity(context.Background(), srv.Client(), cfg, pipeline, 0, 480*60*1000, response)

	sanity, ok := got["l9_sanity"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected l9_sanity block for empty result set with a parse stage, got %#v", got)
	}
	if sanity["matched_count"] != int64(0) {
		t.Errorf("matched_count = %v, want 0", sanity["matched_count"])
	}
	if note, _ := sanity["note"].(string); note == "" {
		t.Error("expected non-empty note for empty result set with a parse stage")
	}
	if *calls != 0 {
		t.Errorf("expected no prometheus baseline call for an empty result set, got %d", *calls)
	}
}

// TestAppendCountSanity_UnparsableCountRowsNotEmptyResultStaysUntouched
// verifies the CRITICAL distinction: !ok can also occur when result rows
// exist but no row carries a parseable count alias (e.g. every "metric" field
// is a group-by label string, not the count). That must stay untouched, not
// be misread as the empty-result-set shape.
func TestAppendCountSanity_UnparsableCountRowsNotEmptyResultStaysUntouched(t *testing.T) {
	srv, calls := promVolumeServer(t, 1000)
	defer srv.Close()

	cfg := sanityTestCfg(t, srv.URL)
	pipeline := []map[string]interface{}{
		{
			"type": "filter",
			"query": map[string]interface{}{
				"$eq": []interface{}{"ServiceName", "orders-service"},
			},
		},
		{
			"type":   "parse",
			"parser": "json",
			"field":  "Body",
		},
		{
			"type": "aggregate",
			"aggregates": []interface{}{
				map[string]interface{}{
					"function": map[string]interface{}{"$count": []interface{}{}},
					"as":       "_count",
				},
			},
		},
	}
	// Row exists, but "metric" carries only a string label — no numeric
	// "_count" field to sum.
	response := aggregateMixedMetricResponse(map[string]any{
		"service": "orders-service",
	})

	got := AppendCountSanity(context.Background(), srv.Client(), cfg, pipeline, 0, 480*60*1000, response)
	if _, ok := got["l9_sanity"]; ok {
		t.Fatal("expected no l9_sanity block when rows exist but no count alias could be parsed")
	}
	if *calls != 0 {
		t.Errorf("expected no prometheus call, got %d", *calls)
	}
}

// TestAppendCountSanity_ZeroMatchedParseStageNoServiceEqAddsSanityNote
// verifies the zero-count guardrail fires even when the pipeline does NOT
// scope by exactly one ServiceName $eq (here: no ServiceName condition at
// all, just a SeverityText filter) — the zero-count/empty-result checks must
// not sit behind ExtractSingleServiceName's gate, since neither zero path
// needs a service name (no PromQL baseline fetch happens for a zero).
func TestAppendCountSanity_ZeroMatchedParseStageNoServiceEqAddsSanityNote(t *testing.T) {
	srv, calls := promVolumeServer(t, 1000)
	defer srv.Close()

	cfg := sanityTestCfg(t, srv.URL)
	pipeline := []map[string]interface{}{
		{
			"type": "filter",
			"query": map[string]interface{}{
				"$eq": []interface{}{"SeverityText", "ERROR"},
			},
		},
		{
			"type":   "parse",
			"parser": "json",
			"field":  "Body",
		},
		{
			"type": "aggregate",
			"aggregates": []interface{}{
				map[string]interface{}{
					"function": map[string]interface{}{"$count": []interface{}{}},
					"as":       "_count",
				},
			},
		},
	}
	response := aggregateCountResponse("_count", float64(0))

	got := AppendCountSanity(context.Background(), srv.Client(), cfg, pipeline, 0, 480*60*1000, response)

	sanity, ok := got["l9_sanity"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected l9_sanity block for zero count without a single ServiceName $eq, got %#v", got)
	}
	if sanity["matched_count"] != int64(0) {
		t.Errorf("matched_count = %v, want 0", sanity["matched_count"])
	}
	if note, _ := sanity["note"].(string); note == "" {
		t.Error("expected non-empty note")
	}
	if *calls != 0 {
		t.Errorf("expected no prometheus baseline call for a zero count, got %d", *calls)
	}
}

// TestAppendCountSanity_ZeroMatchedBodyRegexFilterNoParseStageAddsSanityNote
// verifies the zero-count guardrail widens beyond hasParseStage: the
// parse-free plaintext Body hint (get_log_attributes_for_pipeline's
// recommended one-stage $regex-on-Body shape) has no parse stage at all, so a
// zero from that shape must still get the note.
func TestAppendCountSanity_ZeroMatchedBodyRegexFilterNoParseStageAddsSanityNote(t *testing.T) {
	srv, calls := promVolumeServer(t, 1000)
	defer srv.Close()

	cfg := sanityTestCfg(t, srv.URL)
	pipeline := []map[string]interface{}{
		{
			"type": "filter",
			"query": map[string]interface{}{
				"$and": []interface{}{
					map[string]interface{}{"$eq": []interface{}{"ServiceName", "orders-service"}},
					map[string]interface{}{"$regex": []interface{}{"Body", "timeout.*retry"}},
				},
			},
		},
		{
			"type": "aggregate",
			"aggregates": []interface{}{
				map[string]interface{}{
					"function": map[string]interface{}{"$count": []interface{}{}},
					"as":       "_count",
				},
			},
		},
	}
	response := aggregateCountResponse("_count", float64(0))

	got := AppendCountSanity(context.Background(), srv.Client(), cfg, pipeline, 0, 480*60*1000, response)

	sanity, ok := got["l9_sanity"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected l9_sanity block for zero count from a $regex-on-Body filter with no parse stage, got %#v", got)
	}
	if sanity["matched_count"] != int64(0) {
		t.Errorf("matched_count = %v, want 0", sanity["matched_count"])
	}
	if note, _ := sanity["note"].(string); note == "" {
		t.Error("expected non-empty note")
	}
	if *calls != 0 {
		t.Errorf("expected no prometheus baseline call for a zero count, got %d", *calls)
	}
}

// TestAppendCountSanity_ZeroMatchedGroupbyEmptyResultBodyRegexNoParseStageAddsSanityNote
// covers item 3's test (b): groupby'd $count with zero matches (data.result:
// []), a $regex-on-Body filter, and no parse stage — still gets the note.
func TestAppendCountSanity_ZeroMatchedGroupbyEmptyResultBodyRegexNoParseStageAddsSanityNote(t *testing.T) {
	srv, calls := promVolumeServer(t, 1000)
	defer srv.Close()

	cfg := sanityTestCfg(t, srv.URL)
	pipeline := []map[string]interface{}{
		{
			"type": "filter",
			"query": map[string]interface{}{
				"$and": []interface{}{
					map[string]interface{}{"$eq": []interface{}{"ServiceName", "orders-service"}},
					map[string]interface{}{"$regex": []interface{}{"Body", "timeout.*retry"}},
				},
			},
		},
		{
			"type": "aggregate",
			"aggregates": []interface{}{
				map[string]interface{}{
					"function": map[string]interface{}{"$count": []interface{}{}},
					"as":       "_count",
				},
			},
			"groupby": map[string]interface{}{"ServiceName": "service"},
		},
	}
	response := map[string]interface{}{
		"data": map[string]interface{}{
			"resultType": "matrix",
			"result":     []interface{}{},
		},
	}

	got := AppendCountSanity(context.Background(), srv.Client(), cfg, pipeline, 0, 480*60*1000, response)

	sanity, ok := got["l9_sanity"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected l9_sanity block for empty result set from a $regex-on-Body filter with no parse stage, got %#v", got)
	}
	if sanity["matched_count"] != int64(0) {
		t.Errorf("matched_count = %v, want 0", sanity["matched_count"])
	}
	if note, _ := sanity["note"].(string); note == "" {
		t.Error("expected non-empty note")
	}
	if *calls != 0 {
		t.Errorf("expected no prometheus baseline call for an empty result set, got %d", *calls)
	}
}

// TestAppendCountSanity_ZeroMatchedNeitherParseStageNorBodyLeavesUntouched
// verifies item 3's test (c): a filter with neither a parse stage nor any
// Body condition, returning zero, must leave response untouched exactly as
// before (unremarkable zero on indexed fields only).
func TestAppendCountSanity_ZeroMatchedNeitherParseStageNorBodyLeavesUntouched(t *testing.T) {
	srv, calls := promVolumeServer(t, 1000)
	defer srv.Close()

	cfg := sanityTestCfg(t, srv.URL)
	pipeline := countAggregatePipeline("orders-service")
	response := aggregateCountResponse("_count", float64(0))

	got := AppendCountSanity(context.Background(), srv.Client(), cfg, pipeline, 0, 480*60*1000, response)
	if _, ok := got["l9_sanity"]; ok {
		t.Fatal("expected no l9_sanity block for a zero count with neither a parse stage nor a Body condition")
	}
	if *calls != 0 {
		t.Errorf("expected no prometheus call, got %d", *calls)
	}
}

// TestAppendCountSanity_RawJSONShape verifies at the raw-JSON level (mirroring
// what a client actually receives) that the zero-count l9_sanity block has NO
// "ratio"/"service_log_volume" keys, while the nonzero block still has both.
func TestAppendCountSanity_RawJSONShape(t *testing.T) {
	t.Run("zero count omits ratio and service_log_volume", func(t *testing.T) {
		srv, _ := promVolumeServer(t, 1000)
		defer srv.Close()
		cfg := sanityTestCfg(t, srv.URL)
		pipeline := []map[string]interface{}{
			{
				"type": "filter",
				"query": map[string]interface{}{
					"$eq": []interface{}{"ServiceName", "orders-service"},
				},
			},
			{
				"type":   "parse",
				"parser": "json",
				"field":  "Body",
			},
			{
				"type": "aggregate",
				"aggregates": []interface{}{
					map[string]interface{}{
						"function": map[string]interface{}{"$count": []interface{}{}},
						"as":       "_count",
					},
				},
			},
		}
		response := aggregateCountResponse("_count", float64(0))

		got := AppendCountSanity(context.Background(), srv.Client(), cfg, pipeline, 0, 480*60*1000, response)
		raw, err := json.Marshal(got["l9_sanity"])
		if err != nil {
			t.Fatalf("failed to marshal l9_sanity: %v", err)
		}
		var m map[string]json.RawMessage
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatalf("failed to unmarshal l9_sanity: %v", err)
		}
		if _, ok := m["ratio"]; ok {
			t.Error("zero-count l9_sanity block must NOT have a \"ratio\" key")
		}
		if _, ok := m["service_log_volume"]; ok {
			t.Error("zero-count l9_sanity block must NOT have a \"service_log_volume\" key")
		}
		if _, ok := m["matched_count"]; !ok {
			t.Error("zero-count l9_sanity block must have a \"matched_count\" key")
		}
	})

	t.Run("nonzero count has ratio and service_log_volume", func(t *testing.T) {
		srv, _ := promVolumeServer(t, 1000)
		defer srv.Close()
		cfg := sanityTestCfg(t, srv.URL)
		pipeline := countAggregatePipeline("orders-service")
		response := aggregateCountResponse("_count", float64(750))

		got := AppendCountSanity(context.Background(), srv.Client(), cfg, pipeline, 0, 480*60*1000, response)
		raw, err := json.Marshal(got["l9_sanity"])
		if err != nil {
			t.Fatalf("failed to marshal l9_sanity: %v", err)
		}
		var m map[string]json.RawMessage
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatalf("failed to unmarshal l9_sanity: %v", err)
		}
		if _, ok := m["ratio"]; !ok {
			t.Error("nonzero l9_sanity block must have a \"ratio\" key")
		}
		if _, ok := m["service_log_volume"]; !ok {
			t.Error("nonzero l9_sanity block must have a \"service_log_volume\" key")
		}
	})
}

func TestAppendCountSanity_PromQueryEscapesServiceName(t *testing.T) {
	svc := `orders"service`
	srv, _ := promVolumeServerAssert(t, 1000, func(t *testing.T, r *http.Request) {
		var body struct {
			Query string `json:"query"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}
		// We don't care about the full query encoding here, just that it contains
		// a properly %q-escaped string literal with backslash-escaped quote.
		if !strings.Contains(body.Query, "service_name=\"orders\\\"service\"") {
			t.Fatalf("unexpected query escaping: %q", body.Query)
		}
	})
	defer srv.Close()

	cfg := sanityTestCfg(t, srv.URL)
	pipeline := countAggregatePipeline(svc)
	response := aggregateCountResponse("_count", float64(750))

	_ = AppendCountSanity(context.Background(), srv.Client(), cfg, pipeline, 0, 480*60*1000, response)
}
