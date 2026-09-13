package utils

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestCeilingPromWindowMinutes(t *testing.T) {
	cases := []struct {
		name           string
		startMs, endMs int64
		wantMinutes    int64
	}{
		{"sub_minute_clamps", 0, 30_000, 1},
		{"exact_one_minute", 0, 60_000, 1},
		{"just_over_one_minute", 0, 61_000, 2},
		{"ninety_seconds", 0, 90_000, 2},
		{"exact_five_minutes", 0, 5 * 60_000, 5},
		{"just_under_three_minutes", 0, 179_000, 3},
		{"exact_large_window", 0, 480 * 60_000, 480},
		{"nonzero_start_exact", 1_000_000, 1_000_000 + 120_000, 2},
		{"nonzero_start_ceil", 1_000_000, 1_000_000 + 120_001, 3},
		{"inverted_clamps", 90_000, 0, 1},
		{"zero_duration_clamps", 50, 50, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ceilingPromWindowMinutes(tc.startMs, tc.endMs)
			if got != tc.wantMinutes {
				t.Fatalf("ceilingPromWindowMinutes(%d, %d) = %d, want %d",
					tc.startMs, tc.endMs, got, tc.wantMinutes)
			}
		})
	}
}

var windowMinutesRe = regexp.MustCompile(`\[(\d+)m\]`)

func windowMinutesFromQuery(query string) (int, bool) {
	m := windowMinutesRe.FindStringSubmatch(query)
	if len(m) < 2 {
		return 0, false
	}
	mins, err := strconv.Atoi(m[1])
	if err != nil {
		return 0, false
	}
	return mins, true
}

// volumeProportionalServer returns volume = N * ratePerMinute for a [Nm] query.
func volumeProportionalServer(t *testing.T, ratePerMinute float64) (*httptest.Server, *string) {
	t.Helper()
	var lastQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Query string `json:"query"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		lastQuery = body.Query
		mins, ok := windowMinutesFromQuery(body.Query)
		volume := 0.0
		if ok {
			volume = float64(mins) * ratePerMinute
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp, _ := json.Marshal([]map[string]any{
			{"metric": map[string]string{}, "value": []any{1_700_000_000, volume}},
		})
		_, _ = w.Write(resp)
	}))
	return srv, &lastQuery
}

// timeAwareVolumeServer models emission only in [emitStartSec, emitEndSec].
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
		mins, ok := windowMinutesFromQuery(body.Query)
		vol := 0.0
		if ok {
			anchor := body.Timestamp
			wStart := anchor - int64(mins)*60
			lo, hi := max(wStart, emitStartSec), min(anchor, emitEndSec)
			if hi > lo {
				vol = ratePerSec * float64(hi-lo)
			}
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

// Floor [1m] would inflate 6/100 → 0.06 (false "too broad"); ceil [2m] → 0.03.
func TestCountSanity_CeilingWindow_NonzeroPathNoFalseTooBroad(t *testing.T) {
	srv, lastQuery := volumeProportionalServer(t, 100)
	defer srv.Close()
	cfg := sanityTestCfg(t, srv.URL)
	pipeline := countAggregatePipeline("orders-service")
	response := aggregateCountResponse("_count", float64(6))

	got := AppendCountSanity(context.Background(), srv.Client(), cfg, pipeline, 0, 90*1000, response)
	sanity := got["l9_sanity"].(map[string]interface{})

	if mins, ok := windowMinutesFromQuery(*lastQuery); !ok || mins != 2 {
		t.Fatalf("PromQL window want [2m] (ceiling of 90s); query=%q", *lastQuery)
	}
	if vol, _ := sanity["service_log_volume"].(float64); vol != 200 {
		t.Errorf("service_log_volume = %v, want 200", vol)
	}
	if ratio, _ := sanity["ratio"].(float64); ratio != 0.03 {
		t.Errorf("ratio = %v, want 0.03", ratio)
	}
	if note, _ := sanity["note"].(string); note != "" {
		t.Errorf("expected empty note for ratio 0.03, got: %q", note)
	}
}

// Emission only in [0,30]s of a 90s query: floor [1m] misses it (false genuine
// zero); ceil [2m] covers it and directs toward sample_bodies inspection.
func TestCountSanity_CeilingWindow_ZeroPathSurfacesEmission(t *testing.T) {
	srv, lastQuery := timeAwareVolumeServer(t, 10.0, 0, 30)
	defer srv.Close()
	cfg := sanityTestCfg(t, srv.URL)
	pipeline := parseAndCountPipeline("orders-service")
	response := aggregateCountResponse("_count", float64(0))

	got := AppendCountSanity(context.Background(), srv.Client(), cfg, pipeline, 0, 90*1000, response)
	sanity := got["l9_sanity"].(map[string]interface{})

	if mins, ok := windowMinutesFromQuery(*lastQuery); !ok || mins != 2 {
		t.Fatalf("PromQL window want [2m]; query=%q", *lastQuery)
	}
	if vol, _ := sanity["service_log_volume"].(float64); vol != 300 {
		t.Errorf("service_log_volume = %v, want 300", vol)
	}
	note, _ := sanity["note"].(string)
	if strings.Contains(note, "genuine zero") || strings.Contains(note, "nothing to inspect") {
		t.Errorf("must not classify as genuine zero / nothing to inspect, got: %q", note)
	}
	if !strings.Contains(note, "sample_bodies") {
		t.Errorf("expected sample_bodies guidance, got: %q", note)
	}
}

// Sanity: serviceVolumeBaseline wires the helper into the PromQL selector.
func TestServiceVolumeBaseline_UsesCeilingPromWindow(t *testing.T) {
	var lastQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Query string `json:"query"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		lastQuery = body.Query
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"metric":{},"value":[1,0]}]`))
	}))
	defer srv.Close()
	cfg := sanityTestCfg(t, srv.URL)

	_, ok := serviceVolumeBaseline(context.Background(), srv.Client(), cfg, "svc", 0, 61_000)
	if !ok {
		t.Fatal("expected queryOK")
	}
	want := fmt.Sprintf("[%dm]", ceilingPromWindowMinutes(0, 61_000))
	if !strings.Contains(lastQuery, want) {
		t.Fatalf("PromQL %q missing ceiling selector %s", lastQuery, want)
	}
}
