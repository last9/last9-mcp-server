package logs

import (
	"testing"

	"last9-mcp/internal/utils"
)

func TestMergeLogSearchWindows_DropsWindowLocalStats(t *testing.T) {
	results := []utils.ChunkResult[map[string]any]{
		{Value: map[string]any{
			"data":           map[string]any{"resultType": "streams", "result": []any{}},
			"search_stats":   map[string]any{"bucket_seconds": float64(60), "chunks_planned": float64(2)},
			"volume_summary": map[string]any{"total": float64(4)},
		}},
		{Value: map[string]any{
			"data":           map[string]any{"resultType": "streams", "result": []any{}},
			"search_stats":   map[string]any{"bucket_seconds": float64(60), "chunks_planned": float64(3)},
			"volume_summary": map[string]any{"total": float64(5)},
		}},
	}

	result, err := mergeLogSearchWindows(results, 100)
	if err != nil {
		t.Fatalf("mergeLogSearchWindows: %v", err)
	}
	stats := result["search_stats"].(map[string]any)
	if stats["bucket_seconds"] != float64(60) {
		t.Errorf("bucket_seconds = %v, want 60", stats["bucket_seconds"])
	}
	if stats["chunks_planned"] != float64(5) {
		t.Errorf("chunks_planned = %v, want 5", stats["chunks_planned"])
	}
	if _, present := result["volume_summary"]; present {
		t.Error("volume_summary describes one window and must be absent after merging")
	}
}
