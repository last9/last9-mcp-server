package profiles

import "testing"

func TestUnwrapQueryRangeRowsDataframe(t *testing.T) {
	body := map[string]any{
		"status": "success",
		"data": map[string]any{
			"resultType": "dataframe",
			"result": []any{
				map[string]any{"metric": map[string]any{"name": "svc-a", "samples": float64(10)}},
				map[string]any{"metric": map[string]any{"name": "svc-b", "samples": "5"}},
			},
		},
	}
	rows := unwrapQueryRangeRows(body)
	if len(rows) != 2 {
		t.Fatalf("rows=%d", len(rows))
	}
	if getServiceNameFromRow(rows[0]) != "svc-a" {
		t.Fatalf("row0=%v", rows[0])
	}
	if getRowNumber(rows[1], "samples") != 5 {
		t.Fatalf("samples=%v", rows[1]["samples"])
	}
}

func TestUnwrapQueryRangeRowsPlainArray(t *testing.T) {
	body := []any{
		map[string]any{"ServiceName": "x", "samples": float64(1)},
	}
	rows := unwrapQueryRangeRows(body)
	if len(rows) != 1 || getServiceNameFromRow(rows[0]) != "x" {
		t.Fatalf("rows=%v", rows)
	}
}

func TestBuildProfileServiceIndex(t *testing.T) {
	sampleRows := []map[string]any{
		{"name": "a", "samples": float64(70), "runtime": "go"},
		{"name": "a", "samples": float64(10), "runtime": "java"},
		{"name": "b", "samples": float64(20), "runtime": "go"},
	}
	lastRows := []map[string]any{
		{"name": "a", "last_profile": "2026-08-13T10:00:00Z"},
	}
	out := buildProfileServiceIndex(sampleRows, lastRows)
	if len(out) != 2 {
		t.Fatalf("len=%d", len(out))
	}
	if out[0].Name != "a" || out[0].Samples != 80 || out[0].Runtime != "go" {
		t.Fatalf("first=%+v", out[0])
	}
	if out[0].LastProfileAt == "" {
		t.Fatal("expected last_profile_at")
	}
	if out[0].SharePercent <= out[1].SharePercent {
		t.Fatalf("share order wrong: %+v", out)
	}
}
