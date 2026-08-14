package profiles

import "testing"

func TestFlamegraphPipelinePinsCPUAndService(t *testing.T) {
	pipeline := flamegraphPipeline(ProfileFilters{
		Service:     "api",
		Env:         "prod",
		ProfileType: ProfileTypeCPU,
	}, 100)
	if len(pipeline) != 3 {
		t.Fatalf("stages=%d want 3", len(pipeline))
	}
	filter, ok := pipeline[0].(map[string]any)
	if !ok || filter["type"] != "filter" {
		t.Fatalf("first stage=%v", pipeline[0])
	}
	query, _ := filter["query"].(map[string]any)
	andClause, _ := query["$and"].([]any)
	if len(andClause) != 3 {
		t.Fatalf("and=%v", andClause)
	}
}

func TestNormalizeProfileType(t *testing.T) {
	if normalizeProfileType("") != ProfileTypeCPU {
		t.Fatal("default cpu")
	}
	if normalizeProfileType("ALLOC") != ProfileTypeAlloc {
		t.Fatal("alloc")
	}
	if normalizeProfileType("nope") != ProfileTypeCPU {
		t.Fatal("unknown falls back to cpu")
	}
}
