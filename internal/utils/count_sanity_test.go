package utils

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

func aggregateCountResponse(matchedCount int) map[string]interface{} {
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

func TestAppendCountSanity_HighRatioAddsNote(t *testing.T) {
	srv, _ := promVolumeServer(t, 1000)
	defer srv.Close()

	cfg := sanityTestCfg(t, srv.URL)
	pipeline := countAggregatePipeline("orders-service")
	response := aggregateCountResponse(750)

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
	response := aggregateCountResponse(750)

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
	response := aggregateCountResponse(750)

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
	response := aggregateCountResponse(750)

	got := AppendCountSanity(context.Background(), srv.Client(), cfg, pipeline, 0, 480*60*1000, response)

	if _, ok := got["l9_sanity"]; ok {
		t.Fatal("expected no l9_sanity block when the baseline prometheus query fails")
	}
}
