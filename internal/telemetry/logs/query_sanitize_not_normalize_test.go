package logs

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"last9-mcp/internal/utils"
)

// TestSanitizeNormalizesMapFormNotForBodyHeuristics guards the boundary
// between the sanitizer and the pipeline inspectors: a $not+Body filter must
// report expensive body parsing regardless of whether the caller emitted the
// documented array form {"$not": [condition]} or the map form {"$not": {…}}.
// The sanitizer normalizes the map form to the array form, so both shapes
// reach utils.HasExpensiveBodyParsing identically. Regression test for the
// bug where a map-form $not survived sanitization unchanged and the inspector
// silently skipped it (issue #241).
func TestSanitizeNormalizesMapFormNotForBodyHeuristics(t *testing.T) {
	bodyCond := map[string]interface{}{
		"$contains": []interface{}{"Body", "timeout"},
	}

	cases := []struct {
		name  string
		query map[string]interface{}
	}{
		{
			name: "array_form",
			query: map[string]interface{}{
				"$not": []interface{}{bodyCond},
			},
		},
		{
			name: "map_form",
			query: map[string]interface{}{
				"$not": bodyCond,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sanitized, err := sanitizeLogJSONQuery([]map[string]interface{}{
				{"type": "filter", "query": tc.query},
			})
			if err != nil {
				t.Fatalf("sanitizeLogJSONQuery returned error: %v", err)
			}

			// The map form must have been normalized to the array form.
			notVal := sanitized[0]["query"].(map[string]interface{})["$not"]
			if _, ok := notVal.([]interface{}); !ok {
				t.Fatalf("expected sanitized $not to be []interface{}, got %T", notVal)
			}

			if !utils.HasExpensiveBodyParsing(sanitized) {
				t.Fatalf("%s: expected HasExpensiveBodyParsing=true for a $not+Body pipeline, got false", tc.name)
			}
		})
	}
}

// TestSanitizedMapFormNotReachesCountSanity exercises the full
// sanitize -> count-sanity path: a zero-count aggregate over a map-form
// $not+$regex-on-Body filter must attach the Body-specific l9_sanity
// diagnostic once sanitized, because utils.AppendCountSanity's Body detection
// (pipelineTouchesBody) sees the normalized array-form $not.
func TestSanitizedMapFormNotReachesCountSanity(t *testing.T) {
	// Prometheus baseline server: returns a nonzero service volume so the zero
	// path emits the Body-specific "matched_count is 0 but service_log_volume
	// shows the service emitted logs in this window" note rather than the
	// genuine-zero or ambiguous fallback.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		body, _ := json.Marshal([]map[string]any{
			{"metric": map[string]string{}, "value": []any{1_700_000_000, float64(1000)}},
		})
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	mapFormNotBody := map[string]interface{}{
		"$regex": []interface{}{"Body", "timeout.*retry"},
	}
	stages := []map[string]interface{}{
		{
			"type": "filter",
			"query": map[string]interface{}{
				"$and": []interface{}{
					map[string]interface{}{"$eq": []interface{}{"ServiceName", "orders-service"}},
					// Map-form $not — the shape the sanitizer normalizes.
					map[string]interface{}{"$not": mapFormNotBody},
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

	sanitized, err := sanitizeLogJSONQuery(stages)
	if err != nil {
		t.Fatalf("sanitizeLogJSONQuery returned error: %v", err)
	}

	// Zero-count aggregate response shape (every row {"metric": {_count: 0}}).
	zeroResponse := map[string]interface{}{
		"data": map[string]interface{}{
			"resultType": "matrix",
			"result": []interface{}{
				map[string]interface{}{
					"metric": map[string]interface{}{"_count": float64(0)},
					"values": []interface{}{},
				},
			},
		},
	}

	cfg := testLogsConfig(srv.URL)
	got := utils.AppendCountSanity(context.Background(), srv.Client(), cfg, sanitized, 0, 480*60*1000, zeroResponse)

	sanity, ok := got["l9_sanity"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected l9_sanity block for a zero count from a map-form $not+$regex-on-Body pipeline, got response with no l9_sanity: %#v", got)
	}
	note, _ := sanity["note"].(string)
	if note == "" {
		t.Fatalf("expected a non-empty Body-specific sanity note, got %#v", sanity)
	}
	// The Body-specific diagnostic redirects the user toward Body inspection.
	if !strings.Contains(note, "Body") {
		t.Errorf("expected note to point at Body inspection, got %q", note)
	}
}
