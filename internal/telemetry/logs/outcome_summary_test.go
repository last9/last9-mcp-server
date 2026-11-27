package logs

import "testing"

func TestRecognizedOutcomeFieldFromRawRows(t *testing.T) {
	result := map[string]interface{}{"data": map[string]interface{}{"resultType": "streams", "result": []interface{}{
		map[string]interface{}{"stream": map[string]interface{}{"service": "api", "status_code": "403"}, "values": []interface{}{}},
	}}}
	if got := recognizedOutcomeField(result); got != "status_code" {
		t.Fatalf("field = %q", got)
	}

	result["data"].(map[string]interface{})["result"].([]interface{})[0].(map[string]interface{})["stream"] = map[string]interface{}{"service": "api"}
	if got := recognizedOutcomeField(result); got != "" {
		t.Fatalf("field = %q, want none", got)
	}
}

func TestRecognizedOutcomeFieldUsesStablePriority(t *testing.T) {
	result := map[string]interface{}{"data": map[string]interface{}{"resultType": "streams", "result": []interface{}{
		map[string]interface{}{"stream": map[string]interface{}{"status_code": "403", "status": "denied"}, "values": []interface{}{}},
	}}}
	for range 100 {
		if got := recognizedOutcomeField(result); got != "status" {
			t.Fatalf("field = %q, want status", got)
		}
	}
}

func TestOutcomeCountsFromAggregateRows(t *testing.T) {
	result := map[string]interface{}{"data": map[string]interface{}{"resultType": "streams", "result": []interface{}{
		map[string]interface{}{"stream": map[string]interface{}{"outcome": "403", "count": "8"}},
		map[string]interface{}{"stream": map[string]interface{}{"outcome": "200", "count": 7.0}},
	}}}
	counts := outcomeCounts(result)
	if counts["403"] != 8 || counts["200"] != 7 {
		t.Fatalf("counts = %#v", counts)
	}
}

func TestOutcomeAggregatePipelineKeepsOriginalStages(t *testing.T) {
	query := []map[string]interface{}{{"type": "filter", "query": map[string]interface{}{"$eq": []interface{}{"ServiceName", "api"}}}}
	got := outcomeAggregatePipeline(query, "attributes['status_code']")
	if len(got) != 2 || got[1]["type"] != "aggregate" {
		t.Fatalf("pipeline = %#v", got)
	}
	if len(query) != 1 {
		t.Fatalf("original pipeline mutated: %#v", query)
	}
}
