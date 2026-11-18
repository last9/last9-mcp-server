package logs

import (
	"context"
	"fmt"
	"net/http"

	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"
)

const (
	maxLogSearchWindowMs        = int64(336 * 60 * 60 * 1000)
	maxParallelLogSearchWindows = 3
)

func searchLogsByWindow(
	ctx context.Context, client *http.Client, cfg models.Config, req utils.LogSearchRequest,
) (map[string]any, error) {
	pipeline, _ := req.Pipeline.([]map[string]interface{})
	if req.EndMs-req.StartMs <= maxLogSearchWindowMs || utils.PipelineHasAggregateStage(pipeline) {
		return utils.SearchLogs(ctx, client, cfg, req)
	}

	windows := splitLogSearchWindows(req.StartMs, req.EndMs)
	results := utils.RunChunksParallel(ctx, windows, maxParallelLogSearchWindows,
		func(ctx context.Context, _ int, window utils.TimeChunk) (map[string]any, error) {
			windowReq := req
			windowReq.StartMs = window.StartMs
			windowReq.EndMs = window.EndMs
			return utils.SearchLogs(ctx, client, cfg, windowReq)
		})
	limit := min(req.Limit, utils.LogSearchMaxSampleSize)
	return mergeLogSearchWindows(results, limit)
}

func splitLogSearchWindows(startMs, endMs int64) []utils.TimeChunk {
	windows := make([]utils.TimeChunk, 0, 1+(endMs-startMs-1)/maxLogSearchWindowMs)
	for end := endMs; end > startMs; end -= maxLogSearchWindowMs {
		start := max(end-maxLogSearchWindowMs, startMs)
		windows = append(windows, utils.TimeChunk{StartMs: start, EndMs: end})
	}
	return windows
}

func mergeLogSearchWindows(results []utils.ChunkResult[map[string]any], limit int) (map[string]any, error) {
	var merged map[string]any
	var items []any
	stats := map[string]any{"windows_queried": float64(len(results))}
	errors := make([]any, 0)
	total := 0
	hasTotal := false
	truncated := false
	succeeded := 0
	var firstErr error

	for _, result := range results {
		if result.Err != nil {
			if firstErr == nil {
				firstErr = result.Err
			}
			errors = append(errors, fmt.Sprintf("window %d/%d failed (start_ms=%d end_ms=%d): %v",
				result.Index+1, len(results), result.Chunk.StartMs, result.Chunk.EndMs, result.Err))
			continue
		}
		if merged == nil {
			merged = mapsClone(result.Value)
		}
		_, windowItems, err := extractResultItems(result.Value)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			errors = append(errors, fmt.Sprintf("window %d/%d failed (start_ms=%d end_ms=%d): %v",
				result.Index+1, len(results), result.Chunk.StartMs, result.Chunk.EndMs, err))
			continue
		}
		succeeded++
		items = append(items, windowItems...)
		if windowTotal, ok := result.Value["total_matching_lines"].(int); ok {
			total += windowTotal
			hasTotal = true
		}
		if windowTruncated, ok := result.Value["logs_truncated"].(bool); ok {
			truncated = truncated || windowTruncated
		}
		mergeLogSearchStats(stats, result.Value["search_stats"])
	}

	if merged == nil {
		return nil, fmt.Errorf("all %d log search windows failed (%v): %w", len(results), errors, firstErr)
	}
	data := mapsClone(merged["data"].(map[string]any))
	if limit > 0 && countLogEntriesInResultItems(items) > limit {
		items = truncateResultItemsByEntryLimit(items, limit)
		truncated = true
	}
	data["result"] = items
	merged["data"] = data
	delete(merged, "volume_summary")
	stats["windows_succeeded"] = float64(succeeded)
	stats["windows_failed"] = float64(len(results) - succeeded)
	if len(errors) > 0 {
		stats["errors"] = errors
		delete(stats, "covered_range")
		truncated = true
	}
	merged["search_stats"] = stats
	if hasTotal {
		merged["total_matching_lines"] = total
	}
	merged["logs_truncated"] = truncated
	return merged, nil
}

func mergeLogSearchStats(merged map[string]any, raw any) {
	stats, ok := raw.(map[string]any)
	if !ok {
		return
	}
	for key, value := range stats {
		switch key {
		case "bucket_seconds":
			if _, exists := merged[key]; !exists {
				merged[key] = value
			}
			continue
		case "covered_range":
			mergeLogSearchCoveredRange(merged, value)
			continue
		}
		if number, ok := value.(float64); ok {
			previous, _ := merged[key].(float64)
			merged[key] = previous + number
		} else if _, exists := merged[key]; !exists {
			merged[key] = value
		}
	}
}

func mergeLogSearchCoveredRange(merged map[string]any, value any) {
	covered, ok := value.([]any)
	if !ok || len(covered) != 2 {
		return
	}
	start, startOK := covered[0].(float64)
	end, endOK := covered[1].(float64)
	if !startOK || !endOK {
		return
	}
	current, ok := merged["covered_range"].([]any)
	if !ok || len(current) != 2 {
		merged["covered_range"] = []any{start, end}
		return
	}
	currentStart, startOK := current[0].(float64)
	currentEnd, endOK := current[1].(float64)
	if !startOK || !endOK {
		merged["covered_range"] = []any{start, end}
		return
	}
	merged["covered_range"] = []any{min(currentStart, start), max(currentEnd, end)}
}
