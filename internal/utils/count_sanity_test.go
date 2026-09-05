package utils

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
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

// promEmptySeriesServer mimics the real backend shape for a service that
// emitted no logs at all in the window: physical_index_service_count has NO
// series for that service, so the instant query returns HTTP 200 with an
// EMPTY series list — not a sample carrying an explicit 0. This is the shape
// observed live (see TestAppendCountSanity_ZeroMatchedEmptySeriesBaselineIsGenuineZero).
func promEmptySeriesServer(t *testing.T) (*httptest.Server, *int) {
	t.Helper()
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[]`))
	}))
	return srv, &calls
}

// promMalformedJSONServer returns HTTP 200 with a body that doesn't decode
// into the expected []promInstantVectorPoint shape — a genuine failure to
// get an answer, distinct from a well-formed empty series list.
func promMalformedJSONServer(t *testing.T) (*httptest.Server, *int) {
	t.Helper()
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"not":"an array"}`))
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
// get_log_attributes_for_pipeline's sample_bodies). Since the pipeline scopes
// a single ServiceName, the zero path now attempts the service_log_volume
// baseline to disambiguate; a nonzero baseline (1000) means "logs existed but
// nothing matched" — service_log_volume is attached and the baseline IS
// fetched (contrast with the genuine-zero case, which uses the same baseline
// but a 0 volume).
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
	if sanity["service_log_volume"] != float64(1000) {
		t.Errorf("service_log_volume = %v, want 1000", sanity["service_log_volume"])
	}
	note, _ := sanity["note"].(string)
	if note == "" {
		t.Fatal("expected non-empty note for zero count with a parse stage")
	}
	if strings.Contains(note, "genuine zero") {
		t.Errorf("nonzero-volume zero-count note must not claim a genuine zero, got: %q", note)
	}
	if *calls != 1 {
		t.Errorf("expected exactly one prometheus baseline call for a zero count with a resolvable service, got %d", *calls)
	}
}

// TestAppendCountSanity_ZeroMatchedGroupbyEmptyResultAddsSanityNote verifies
// the empty-result-set shape a groupby'd $count with zero matches actually
// takes (data.result: []), as opposed to a single zero-valued row, still gets
// the same zero-count l9_sanity block when the pipeline has a parse stage —
// and, since a single ServiceName is resolvable, also gets the nonzero
// service_log_volume baseline attached (logs existed, so a genuine zero is
// ruled out).
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
	if sanity["service_log_volume"] != float64(1000) {
		t.Errorf("service_log_volume = %v, want 1000", sanity["service_log_volume"])
	}
	if note, _ := sanity["note"].(string); note == "" {
		t.Error("expected non-empty note for empty result set with a parse stage")
	}
	if *calls != 1 {
		t.Errorf("expected exactly one prometheus baseline call for an empty result set with a resolvable service, got %d", *calls)
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
	if sanity["service_log_volume"] != float64(1000) {
		t.Errorf("service_log_volume = %v, want 1000", sanity["service_log_volume"])
	}
	if note, _ := sanity["note"].(string); note == "" {
		t.Error("expected non-empty note")
	}
	if *calls != 1 {
		t.Errorf("expected exactly one prometheus baseline call for a zero count with a resolvable service, got %d", *calls)
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
	if sanity["service_log_volume"] != float64(1000) {
		t.Errorf("service_log_volume = %v, want 1000", sanity["service_log_volume"])
	}
	if note, _ := sanity["note"].(string); note == "" {
		t.Error("expected non-empty note")
	}
	if *calls != 1 {
		t.Errorf("expected exactly one prometheus baseline call for an empty result set with a resolvable service, got %d", *calls)
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
// what a client actually receives) that a zero-count block whose service
// can't be resolved to a single value has NO "ratio"/"service_log_volume"
// keys (the ambiguous fallback, unchanged from before the volume-baseline
// discriminator existed), while the nonzero ratio-path block still has both.
func TestAppendCountSanity_RawJSONShape(t *testing.T) {
	t.Run("zero count with unresolvable service omits ratio and service_log_volume", func(t *testing.T) {
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
		if *calls != 0 {
			t.Errorf("expected no prometheus baseline call when the service can't be resolved, got %d", *calls)
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

// TestAppendCountSanity_ZeroMatchedGenuineZeroVolumeAddsGenuineZeroNote
// verifies that when the service_log_volume baseline itself comes back 0
// (the service emitted no logs at all in the window), the zero-count block
// says so explicitly — with service_log_volume: 0 — and does NOT tell the
// model to re-check its parse or inspect sample_bodies, since there is
// nothing to inspect.
func TestAppendCountSanity_ZeroMatchedGenuineZeroVolumeAddsGenuineZeroNote(t *testing.T) {
	srv, calls := promVolumeServer(t, 0)
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
		t.Fatalf("expected l9_sanity block for a genuine zero, got %#v", got)
	}
	if sanity["matched_count"] != int64(0) {
		t.Errorf("matched_count = %v, want 0", sanity["matched_count"])
	}
	if sanity["service_log_volume"] != float64(0) {
		t.Errorf("service_log_volume = %v, want 0", sanity["service_log_volume"])
	}
	note, _ := sanity["note"].(string)
	if note == "" {
		t.Fatal("expected non-empty note for a genuine zero")
	}
	if strings.Contains(note, "parse stage / Body regex-or-contains not matching") ||
		strings.Contains(note, "inspect sample_bodies") {
		t.Errorf("genuine-zero note must not instruct re-checking the parse or inspecting samples, got: %q", note)
	}
	if *calls != 1 {
		t.Errorf("expected exactly one prometheus baseline call, got %d", *calls)
	}
}

// TestAppendCountSanity_ZeroMatchedBaselineFetchFailureFallsBackToAmbiguousNote
// verifies that when the service resolves to a single value but the
// service_log_volume baseline fetch itself fails, the zero-count block falls
// back to the CURRENT ambiguous note with no service_log_volume/ratio keys —
// never fails the tool.
func TestAppendCountSanity_ZeroMatchedBaselineFetchFailureFallsBackToAmbiguousNote(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"boom"}`))
	}))
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
		t.Fatalf("expected l9_sanity block even when the baseline fetch fails, got %#v", got)
	}
	if sanity["matched_count"] != int64(0) {
		t.Errorf("matched_count = %v, want 0", sanity["matched_count"])
	}
	if _, ok := sanity["service_log_volume"]; ok {
		t.Errorf("expected no service_log_volume key when the baseline fetch fails, got: %#v", sanity)
	}
	if note, _ := sanity["note"].(string); note == "" {
		t.Error("expected the ambiguous fallback note when the baseline fetch fails")
	}
}

// TestAppendCountSanity_ZeroMatchedEmptySeriesBaselineIsGenuineZero is the
// PRIMARY regression test for the live bug: a service that emitted no logs at
// all has NO series in physical_index_service_count, so the baseline instant
// query returns HTTP 200 with an EMPTY series list — not an explicit
// zero-valued sample. Before the fix, serviceVolumeBaseline reported
// found=false for this shape and the caller fell back to the ambiguous note
// with no service_log_volume key. This must now behave identically to the
// explicit-zero-sample case: genuine-zero note with service_log_volume: 0.
func TestAppendCountSanity_ZeroMatchedEmptySeriesBaselineIsGenuineZero(t *testing.T) {
	srv, calls := promEmptySeriesServer(t)
	defer srv.Close()

	cfg := sanityTestCfg(t, srv.URL)
	pipeline := []map[string]interface{}{
		{
			"type": "filter",
			"query": map[string]interface{}{
				"$and": []interface{}{
					map[string]interface{}{"$eq": []interface{}{"ServiceName", "__no_such_service__"}},
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
		t.Fatalf("expected l9_sanity block for an empty-series baseline, got %#v", got)
	}
	if sanity["matched_count"] != int64(0) {
		t.Errorf("matched_count = %v, want 0", sanity["matched_count"])
	}
	if sanity["service_log_volume"] != float64(0) {
		t.Errorf("service_log_volume = %v, want 0 (empty series = genuine zero, not ambiguous)", sanity["service_log_volume"])
	}
	note, _ := sanity["note"].(string)
	if note == "" {
		t.Fatal("expected non-empty note for an empty-series genuine zero")
	}
	if strings.Contains(note, "parse stage / Body regex-or-contains not matching") ||
		strings.Contains(note, "inspect sample_bodies") {
		t.Errorf("genuine-zero note must not instruct re-checking the parse or inspecting samples, got: %q", note)
	}
	if *calls != 1 {
		t.Errorf("expected exactly one prometheus baseline call, got %d", *calls)
	}
}

// TestAppendCountSanity_ZeroMatchedBaselineMalformedJSONFallsBackToAmbiguousNote
// verifies a genuine decode failure (HTTP 200 but a body that doesn't decode
// into the expected shape) still falls back to the ambiguous note with no
// service_log_volume key — distinct from the well-formed empty-series case.
func TestAppendCountSanity_ZeroMatchedBaselineMalformedJSONFallsBackToAmbiguousNote(t *testing.T) {
	srv, _ := promMalformedJSONServer(t)
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
		t.Fatalf("expected l9_sanity block even when the baseline body is malformed, got %#v", got)
	}
	if _, ok := sanity["service_log_volume"]; ok {
		t.Errorf("expected no service_log_volume key when the baseline decode fails, got: %#v", sanity)
	}
	if note, _ := sanity["note"].(string); note == "" {
		t.Error("expected the ambiguous fallback note when the baseline decode fails")
	}
}

// TestAppendCountSanity_NonzeroMatchedEmptySeriesBaselineLeavesResponseUntouched
// is the regression guard for the nonzero/ratio path: a nonzero matched count
// with an empty-series (now queryOK=true, volume=0) baseline must behave
// exactly as it always has for an unavailable/zero baseline — response
// returned untouched, no l9_sanity block, no divide-by-zero.
func TestAppendCountSanity_NonzeroMatchedEmptySeriesBaselineLeavesResponseUntouched(t *testing.T) {
	srv, calls := promEmptySeriesServer(t)
	defer srv.Close()

	cfg := sanityTestCfg(t, srv.URL)
	pipeline := countAggregatePipeline("orders-service")
	response := aggregateCountResponse("_count", float64(750))

	got := AppendCountSanity(context.Background(), srv.Client(), cfg, pipeline, 0, 480*60*1000, response)

	if _, ok := got["l9_sanity"]; ok {
		t.Fatalf("expected no l9_sanity block for a nonzero count with an empty-series baseline, got %#v", got)
	}
	if *calls != 1 {
		t.Errorf("expected exactly one prometheus baseline call, got %d", *calls)
	}
}

// ---------------------------------------------------------------------------
// Tests for the ceiling-window fix (non-whole-minute query windows).
//
// The guardrail compares a $count aggregate's matched count (measured over the
// exact [startMs, endMs] query window by the log aggregate API) against a
// service-volume baseline fetched via PromQL sum_over_time(...[Nm]) anchored at
// endMs. Floor division for N (the old behaviour) dropped up to ~59 s of the
// query window for non-whole-minute durations, inflating the ratio on the
// nonzero path (false "too broad" notes) and misclassifying a truncated-but-
// emitting service as a "genuine zero" on the zero path. The fix ceiling-
// divides so the baseline window is never shorter than the query window.
// ---------------------------------------------------------------------------

var windowMinutesRe = regexp.MustCompile(`\[(\d+)m\]`)

// volumeProportionalServer models a service emitting at a constant ratePerMinute.
// For a [Nm] PromQL window it returns volume = N * ratePerMinute. It captures
// the last query string so callers can assert on the window selector the
// guardrail chose.
func volumeProportionalServer(t *testing.T, ratePerMinute float64) (*httptest.Server, *string) {
	t.Helper()
	var lastQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Query string `json:"query"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		lastQuery = body.Query
		m := windowMinutesRe.FindStringSubmatch(body.Query)
		mins, _ := strconv.Atoi(m[1])
		volume := float64(mins) * ratePerMinute
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp, _ := json.Marshal([]map[string]any{
			{"metric": map[string]string{}, "value": []any{1_700_000_000, volume}},
		})
		_, _ = w.Write(resp)
	}))
	return srv, &lastQuery
}

// timeAwareVolumeServer models a service emitting at ratePerSec during
// [emitStartSec, emitEndSec] (in seconds) and nothing otherwise. It reads both
// the [Nm] window selector and the timestamp anchor from the guardrail's
// instant-query body and returns the volume a real sum_over_time would yield:
// rate * overlap([anchor - N*60, anchor], [emitStart, emitEnd]). This lets
// tests prove that a ceiling window covers the emission while a floor window
// would have dropped it.
func timeAwareVolumeServer(t *testing.T, ratePerSec float64, emitStartSec, emitEndSec int64) (*httptest.Server, *string) {
	t.Helper()
	var lastQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Query     string `json:"query"`
			Timestamp int64  `json:"timestamp"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		lastQuery = body.Query
		m := windowMinutesRe.FindStringSubmatch(body.Query)
		mins, _ := strconv.Atoi(m[1])
		anchor := body.Timestamp
		wStart := anchor - int64(mins)*60
		lo, hi := max(wStart, emitStartSec), min(anchor, emitEndSec)
		vol := 0.0
		if hi > lo {
			vol = ratePerSec * float64(hi-lo)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp, _ := json.Marshal([]map[string]any{
			{"metric": map[string]string{}, "value": []any{1_700_000_000, vol}},
		})
		_, _ = w.Write(resp)
	}))
	return srv, &lastQuery
}

// parseAndCountPipeline builds a pipeline that filters by a single service,
// parses the Body as JSON, and runs a $count aggregate — the parse-mismatch
// shape the zero-count guardrail is designed to surface.
func parseAndCountPipeline(service string) []map[string]interface{} {
	return []map[string]interface{}{
		{
			"type": "filter",
			"query": map[string]interface{}{
				"$eq": []interface{}{"ServiceName", service},
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
}

// TestCountSanity_CeilingWindowForSubMinuteQuery_NonzeroPath verifies that a
// 90 s query uses [2m] (ceiling), not [1m] (floor), and that the inflated
// ratio false-positive ("too broad" note) no longer fires for a window whose
// true ratio is below the 5 % threshold.
//
// With a constant-rate server at 100 lines/min:
//   - True window is 90 s → 150 lines; 6 matches → true ratio 0.04 (no note).
//   - Old floor [1m]: volume 100 → ratio 0.06 (false "too broad" note).
//   - Fix  ceil [2m]: volume 200 → ratio 0.03 (no note — conservative).
func TestCountSanity_CeilingWindowForSubMinuteQuery_NonzeroPath(t *testing.T) {
	srv, lastQuery := volumeProportionalServer(t, 100)
	defer srv.Close()
	cfg := sanityTestCfg(t, srv.URL)
	pipeline := countAggregatePipeline("orders-service")
	response := aggregateCountResponse("_count", float64(6))

	got := AppendCountSanity(context.Background(), srv.Client(), cfg, pipeline, 0, 90*1000, response)
	sanity := got["l9_sanity"].(map[string]interface{})

	if !strings.Contains(*lastQuery, "[2m]") {
		t.Fatalf("expected PromQL [2m] (ceiling) for a 90 s query, got: %q", *lastQuery)
	}
	if strings.Contains(*lastQuery, "[1m]") {
		t.Fatalf("PromQL must NOT use floor [1m] for a 90 s query, got: %q", *lastQuery)
	}
	if vol, _ := sanity["service_log_volume"].(float64); vol != 200 {
		t.Errorf("service_log_volume = %v, want 200 (2m * 100/min)", vol)
	}
	if ratio, _ := sanity["ratio"].(float64); ratio != 0.03 {
		t.Errorf("ratio = %v, want 0.03 (6/200)", ratio)
	}
	if note, _ := sanity["note"].(string); note != "" {
		t.Errorf("expected empty note for ratio 0.03 (< 5%%), got: %q", note)
	}
}

// TestCountSanity_CeilingWindowForSubMinuteQuery_ZeroPathNoFalseGenuineZero
// verifies the zero-path fix: a service that emitted only in the prefix of
// the query window (the part that floor division would have dropped) is NOT
// misclassified as a "genuine zero".
//
// Service emits 10 lines/sec during [0 s, 30 s]; query window is [0 s, 90 s].
//   - Old floor [1m]: baseline [30 s, 90 s] → volume 0 → false "genuine
//     zero / nothing to inspect".
//   - Fix  ceil [2m]: baseline [-30 s, 90 s] → volume 300 → "parse/filter
//     mismatch, inspect sample_bodies".
func TestCountSanity_CeilingWindowForSubMinuteQuery_ZeroPathNoFalseGenuineZero(t *testing.T) {
	srv, lastQuery := timeAwareVolumeServer(t, 10.0, 0, 30)
	defer srv.Close()
	cfg := sanityTestCfg(t, srv.URL)
	pipeline := parseAndCountPipeline("orders-service")
	response := aggregateCountResponse("_count", float64(0))

	got := AppendCountSanity(context.Background(), srv.Client(), cfg, pipeline, 0, 90*1000, response)
	sanity := got["l9_sanity"].(map[string]interface{})

	if !strings.Contains(*lastQuery, "[2m]") {
		t.Fatalf("expected PromQL [2m] (ceiling) for a 90 s query, got: %q", *lastQuery)
	}
	if vol, _ := sanity["service_log_volume"].(float64); vol != 300 {
		t.Errorf("service_log_volume = %v, want 300 (30 s * 10/s overlap with [-30 s, 90 s])", vol)
	}
	note, _ := sanity["note"].(string)
	if strings.Contains(note, "genuine zero") {
		t.Errorf("must NOT classify as genuine zero when the baseline saw the emission, got: %q", note)
	}
	if strings.Contains(note, "nothing to inspect") {
		t.Errorf("must NOT direct the model away from inspection when logs existed, got: %q", note)
	}
	if !strings.Contains(note, "sample_bodies") {
		t.Errorf("expected the note to direct toward sample_bodies inspection, got: %q", note)
	}
}

// TestCountSanity_WholeMinuteWindowUsesExactMinutes verifies that a whole-
// minute window is unaffected by the ceiling fix: ceiling(N) == N for whole
// minutes, so the baseline window must stay exact (no over-rounding).
func TestCountSanity_WholeMinuteWindowUsesExactMinutes(t *testing.T) {
	srv, lastQuery := volumeProportionalServer(t, 100)
	defer srv.Close()
	cfg := sanityTestCfg(t, srv.URL)
	pipeline := countAggregatePipeline("orders-service")
	response := aggregateCountResponse("_count", float64(50))

	got := AppendCountSanity(context.Background(), srv.Client(), cfg, pipeline, 0, 5*60*1000, response)

	if !strings.Contains(*lastQuery, "[5m]") {
		t.Fatalf("expected PromQL [5m] for a 5-minute query, got: %q", *lastQuery)
	}
	if strings.Contains(*lastQuery, "[6m]") {
		t.Fatalf("PromQL must NOT over-round to [6m] for a 5-minute query, got: %q", *lastQuery)
	}
	sanity := got["l9_sanity"].(map[string]interface{})
	if vol, _ := sanity["service_log_volume"].(float64); vol != 500 {
		t.Errorf("service_log_volume = %v, want 500 (5m * 100/min)", vol)
	}
	if ratio, _ := sanity["ratio"].(float64); ratio != 0.1 {
		t.Errorf("ratio = %v, want 0.1 (50/500)", ratio)
	}
	if note, _ := sanity["note"].(string); note == "" {
		t.Error("expected a too-broad note for ratio 0.1 (> 5%)")
	}
}

// TestCountSanity_ExactlyOneMinuteUsesOneMinute verifies the boundary: a
// 60 000 ms (exactly 1 minute) window ceilings to [1m], not [2m].
func TestCountSanity_ExactlyOneMinuteUsesOneMinute(t *testing.T) {
	srv, lastQuery := volumeProportionalServer(t, 100)
	defer srv.Close()
	cfg := sanityTestCfg(t, srv.URL)
	pipeline := countAggregatePipeline("orders-service")
	response := aggregateCountResponse("_count", float64(2))

	_ = AppendCountSanity(context.Background(), srv.Client(), cfg, pipeline, 0, 60*1000, response)

	if !strings.Contains(*lastQuery, "[1m]") {
		t.Fatalf("expected [1m] for a 60 s query (exact), got: %q", *lastQuery)
	}
	if strings.Contains(*lastQuery, "[2m]") {
		t.Fatalf("PromQL must NOT over-round to [2m] for an exact 60 s query, got: %q", *lastQuery)
	}
}

// TestCountSanity_JustOverOneMinuteCeilsToTwoMinutes verifies that a window
// just past a minute boundary (61 000 ms) ceilings to [2m] instead of flooring
// to [1m].
func TestCountSanity_JustOverOneMinuteCeilsToTwoMinutes(t *testing.T) {
	srv, lastQuery := volumeProportionalServer(t, 100)
	defer srv.Close()
	cfg := sanityTestCfg(t, srv.URL)
	pipeline := countAggregatePipeline("orders-service")
	response := aggregateCountResponse("_count", float64(10))

	_ = AppendCountSanity(context.Background(), srv.Client(), cfg, pipeline, 0, 61000, response)

	if !strings.Contains(*lastQuery, "[2m]") {
		t.Fatalf("expected [2m] (ceiling) for a 61 s query, got: %q", *lastQuery)
	}
	if strings.Contains(*lastQuery, "[1m]") {
		t.Fatalf("PromQL must NOT use floor [1m] for a 61 s query, got: %q", *lastQuery)
	}
}

// TestCountSanity_SubMinuteWindowClampsToOneMinute verifies that a sub-minute
// window (e.g. 30 s) still clamps to the 1-minute floor (PromQL cannot express
// a [0m] selector). This is unchanged from the old behaviour for short windows.
func TestCountSanity_SubMinuteWindowClampsToOneMinute(t *testing.T) {
	srv, lastQuery := volumeProportionalServer(t, 100)
	defer srv.Close()
	cfg := sanityTestCfg(t, srv.URL)
	pipeline := countAggregatePipeline("orders-service")
	response := aggregateCountResponse("_count", float64(2))

	_ = AppendCountSanity(context.Background(), srv.Client(), cfg, pipeline, 0, 30*1000, response)

	if !strings.Contains(*lastQuery, "[1m]") {
		t.Fatalf("expected [1m] for a 30 s query (clamped to min 1), got: %q", *lastQuery)
	}
}

// TestCountSanity_ZeroPathCeilingSurfacesEmissionDroppedByFloor provides the
// full end-to-end proof for the zero-path fix. A service emits only in the
// prefix the old floor would have dropped; the ceiling baseline sees the
// emission, and the guardrail directs the model to inspect sample_bodies
// (the parse-mismatch branch) rather than directing it away ("nothing to
// inspect").
//
// Query window [0 s, 90 s]; emission [0 s, 30 s] at 10 lines/sec.
// Ceiling [2m] baseline = [-30 s, 90 s] → overlap [0, 30] → volume 300.
func TestCountSanity_ZeroPathCeilingSurfacesEmissionDroppedByFloor(t *testing.T) {
	srv, lastQuery := timeAwareVolumeServer(t, 10.0, 0, 30)
	defer srv.Close()
	cfg := sanityTestCfg(t, srv.URL)
	pipeline := parseAndCountPipeline("orders-service")
	response := aggregateCountResponse("_count", float64(0))

	got := AppendCountSanity(context.Background(), srv.Client(), cfg, pipeline, 0, 90*1000, response)
	sanity := got["l9_sanity"].(map[string]interface{})

	if sanity["matched_count"] != int64(0) {
		t.Errorf("matched_count = %v, want 0", sanity["matched_count"])
	}
	if !strings.Contains(*lastQuery, "[2m]") {
		t.Fatalf("expected ceiling [2m], got: %q", *lastQuery)
	}
	vol, _ := sanity["service_log_volume"].(float64)
	if vol <= 0 {
		t.Fatalf("service_log_volume = %v, want > 0 (ceiling baseline must cover the emission)", vol)
	}
	note, _ := sanity["note"].(string)
	if !strings.Contains(note, "sample_bodies") {
		t.Errorf("expected the note to direct toward sample_bodies inspection (parse mismatch), got: %q", note)
	}
	if strings.Contains(note, "nothing to inspect") {
		t.Errorf("must NOT direct model away from inspection, got: %q", note)
	}
}

// TestCountSanity_NonzeroPathCeilingDeflatesRatioForTwoMinuteWindow verifies
// the conservative direction of the ceiling fix on the nonzero path for a
// window just under 3 minutes: the ratio is deflated (not inflated), so a
// true ratio sitting just below the 5 % threshold does NOT flip to a false
// "too broad" note.
//
// 179 s window, 100 lines/min, 9 matches.
//   - True window 179 s → ~298.3 lines; ratio 9/298.3 ≈ 0.0302 (no note).
//   - Old floor [2m]: volume 200 → ratio 0.045 (no note, but dangerously close).
//   - Fix  ceil [3m]: volume 300 → ratio 0.03 (no note — even safer).
func TestCountSanity_NonzeroPathCeilingDeflatesRatioForTwoMinuteWindow(t *testing.T) {
	srv, lastQuery := volumeProportionalServer(t, 100)
	defer srv.Close()
	cfg := sanityTestCfg(t, srv.URL)
	pipeline := countAggregatePipeline("orders-service")
	response := aggregateCountResponse("_count", float64(9))

	got := AppendCountSanity(context.Background(), srv.Client(), cfg, pipeline, 0, 179*1000, response)
	sanity := got["l9_sanity"].(map[string]interface{})

	if !strings.Contains(*lastQuery, "[3m]") {
		t.Fatalf("expected [3m] (ceiling of 179 s) for a 179 s query, got: %q", *lastQuery)
	}
	if strings.Contains(*lastQuery, "[2m]") {
		t.Fatalf("PromQL must NOT use floor [2m] for a 179 s query, got: %q", *lastQuery)
	}
	if vol, _ := sanity["service_log_volume"].(float64); vol != 300 {
		t.Errorf("service_log_volume = %v, want 300 (3m * 100/min)", vol)
	}
	if ratio, _ := sanity["ratio"].(float64); ratio >= 0.05 {
		t.Errorf("ratio = %v, must be < 0.05 after ceiling fix (no false too-broad note)", ratio)
	}
	if note, _ := sanity["note"].(string); note != "" {
		t.Errorf("expected empty note for ratio < 5%%, got: %q", note)
	}
}

// TestCountSanity_LargeWholeMinuteWindowUnchangedByCeiling verifies that a
// large whole-minute window (480 minutes) is unaffected by the ceiling fix —
// the PromQL selector and the computed ratio stay the same as before.
func TestCountSanity_LargeWholeMinuteWindowUnchangedByCeiling(t *testing.T) {
	srv, lastQuery := volumeProportionalServer(t, 100)
	defer srv.Close()
	cfg := sanityTestCfg(t, srv.URL)
	pipeline := countAggregatePipeline("orders-service")
	response := aggregateCountResponse("_count", float64(750))

	got := AppendCountSanity(context.Background(), srv.Client(), cfg, pipeline, 0, 480*60*1000, response)
	sanity := got["l9_sanity"].(map[string]interface{})

	if !strings.Contains(*lastQuery, "[480m]") {
		t.Fatalf("expected [480m] for a 480-minute query (exact, unchanged), got: %q", *lastQuery)
	}
	if strings.Contains(*lastQuery, "[481m]") {
		t.Fatalf("PromQL must NOT over-round to [481m] for a 480-minute query, got: %q", *lastQuery)
	}
	if vol, _ := sanity["service_log_volume"].(float64); vol != 48000 {
		t.Errorf("service_log_volume = %v, want 48000 (480m * 100/min)", vol)
	}
	if ratio, _ := sanity["ratio"].(float64); ratio != 0.0156 {
		t.Errorf("ratio = %v, want 0.0156 (750/48000 rounded to 4 dp)", ratio)
	}
	if note, _ := sanity["note"].(string); note != "" {
		t.Errorf("expected empty note for ratio 0.0156 (< 5%%), got: %q", note)
	}
}
