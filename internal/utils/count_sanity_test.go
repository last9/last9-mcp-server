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

func TestAppendCountSanity_ZeroMatchedSkipsAndNoPromCall(t *testing.T) {
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
