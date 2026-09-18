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

func TestParseProfileType(t *testing.T) {
	pt, err := parseProfileType("")
	if err != nil || pt != ProfileTypeCPU {
		t.Fatalf("default cpu: %v %v", pt, err)
	}
	pt, err = parseProfileType("ALLOC")
	if err != nil || pt != ProfileTypeAlloc {
		t.Fatalf("alloc: %v %v", pt, err)
	}
	if _, err := parseProfileType("nope"); err == nil {
		t.Fatal("expected error for unknown profile_type")
	}
}

func TestClampFlamegraphLimit(t *testing.T) {
	if clampFlamegraphLimit(0) != DefaultFlamegraphRowLimit {
		t.Fatal("default")
	}
	if clampFlamegraphLimit(50000) != MaxFlamegraphRowLimit {
		t.Fatal("cap")
	}
	if clampFlamegraphLimit(42) != 42 {
		t.Fatal("passthrough")
	}
}
