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

// TestNotShapeReproLog demonstrates that a map-form {"$not": {...}} survives
// sanitizeLogCondition unchanged and is then silently skipped by the inspect
// heuristics that downstream consumers (chunking throttle via
// utils.HasExpensiveBodyParsing, count-sanity diagnostic via
// utils.pipelineTouchesBody) rely on. The inspector only descends into $not
// when its value is []any (pipeline_inspect.go), so a map-form $not that
// filters on Body reports HasExpensiveBodyParsing=false despite filtering on
// Body. The same query in the documented array form reports true. The sole
// cause of the divergence is the map-vs-array shape of $not.
//
// This test serves as the in-process reproduction referenced in the bug
// report. After the sanitizer normalizes map-form $not to the single-element
// array form, all four cases return true and the divergence disappears.
func TestNotShapeReproLog(t *testing.T) {
	bodyCond := map[string]interface{}{
		"$contains": []interface{}{"Body", "timeout"},
	}

	cases := []struct {
		name  string
		query map[string]interface{}
	}{
		{
			name: "toplevel_array_form",
			query: map[string]interface{}{
				"$not": []interface{}{bodyCond},
			},
		},
		{
			name: "toplevel_map_form",
			query: map[string]interface{}{
				"$not": bodyCond,
			},
		},
		{
			name: "nested_array_form",
			query: map[string]interface{}{
				"$and": []interface{}{
					map[string]interface{}{
						"$not": []interface{}{bodyCond},
					},
				},
			},
		},
		{
			name: "nested_map_form",
			query: map[string]interface{}{
				"$and": []interface{}{
					map[string]interface{}{
						"$not": bodyCond,
					},
				},
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

			got := utils.HasExpensiveBodyParsing(sanitized)
			// After the sanitizer normalizes map-form $not to the array form,
			// every shape must report expensive body parsing, because each
			// filters on Body with a non-indexable ($contains) operator.
			if !got {
				t.Fatalf("%s: expected HasExpensiveBodyParsing=true for a $not+Body pipeline, got false (map-form $not was skipped by the inspector)", tc.name)
			}
		})
	}
}

// TestNotShapeReproLogCountSanity verifies the second downstream heuristic:
// utils.pipelineTouchesBody (unexported, exercised through utils.AppendCountSanity).
// A count-aggregate pipeline over a map-form $not+$regex-on-Body that returns a
// zero count must attach the Body-specific l9_sanity diagnostic. Before the
// sanitizer normalization, pipelineTouchesBody skipped the map-form $not and
// AppendCountSanity returned the response with no l9_sanity block at all,
// silently dropping the diagnostic the guardrail exists to surface. The
// pipeline is sanitized through the same sanitizer the get_logs handler uses,
// so this test exercises the full sanitize -> count-sanity path end to end.
func TestNotShapeReproLogCountSanity(t *testing.T) {
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
					// Map-form $not — the shape free-form callers may emit and
					// the one the sanitizer must normalize to the array form.
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

	// Sanity-check the sanitizer really normalized the map form: the $not
	// inside $and must now be a single-element array, exactly the shape the
	// inspector descends into.
	andConds := sanitized[0]["query"].(map[string]interface{})["$and"].([]interface{})
	notVal := andConds[1].(map[string]interface{})["$not"]
	if _, ok := notVal.([]interface{}); !ok {
		t.Fatalf("expected sanitized $not to be []interface{}, got %T", notVal)
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
		t.Fatalf("expected l9_sanity block for a zero count from a map-form $not+$regex-on-Body pipeline (pipelineTouchesBody should see the normalized Body condition), got response with no l9_sanity: %#v", got)
	}
	note, _ := sanity["note"].(string)
	if note == "" {
		t.Fatalf("expected a non-empty Body-specific sanity note, got %#v", sanity)
	}
	// The Body-specific diagnostic redirects the user toward Body inspection —
	// the guidance the count-sanity guardrail exists to surface and that the
	// pre-fix map-form $not would silently drop.
	if !strings.Contains(note, "Body") {
		t.Errorf("expected note to point at Body inspection, got %q", note)
	}
}
