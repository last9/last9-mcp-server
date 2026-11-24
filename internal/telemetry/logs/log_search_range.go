package logs

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"
)

const (
	maxLogSearchWindowHours     = 336
	maxLogSearchWindowMs        = int64(maxLogSearchWindowHours * time.Hour / time.Millisecond)
	maxParallelLogSearchWindows = 3
)

const (
	logSearchPieceComplete = "complete"
	logSearchPiecePartial  = "partial"
	logSearchPieceFailed   = "failed"
)

// Counts or sums could be added across pieces, but percentiles, averages and
// top-N cannot, and the pipeline does not say which one it is.
var errAggregateOverLogSearchWindow = fmt.Errorf(
	"aggregate log queries can read at most %d hours (14 days) at once, and their results are not combined across reads: "+
		"narrow the time range to %d hours or less, or run one query per window of at most %d hours "+
		"and add the results yourself only when the aggregate is a count or sum",
	maxLogSearchWindowHours, maxLogSearchWindowHours, maxLogSearchWindowHours)

func searchLogsByWindow(
	ctx context.Context, client *http.Client, cfg models.Config, req utils.LogSearchRequest,
) (map[string]any, error) {
	pipeline, _ := req.Pipeline.([]map[string]interface{})
	if req.EndMs-req.StartMs <= maxLogSearchWindowMs {
		return utils.SearchLogs(ctx, client, cfg, req)
	}
	if utils.PipelineHasAggregateStage(pipeline) {
		return nil, errAggregateOverLogSearchWindow
	}

	windows := splitLogSearchWindows(req.StartMs, req.EndMs)
	results := utils.RunChunksParallel(ctx, windows, maxParallelLogSearchWindows,
		func(ctx context.Context, _ int, window utils.TimeChunk) (map[string]any, error) {
			windowReq := req
			windowReq.StartMs = window.StartMs
			windowReq.EndMs = window.EndMs
			return utils.SearchLogs(ctx, client, cfg, windowReq)
		})
	// The caller has gone: windows that finished first are not a partial answer.
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("%s cancelled: %w", req.Op, err)
	}
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
	pieces := make([]any, 0, len(results))
	entriesBefore := 0
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
			pieces = append(pieces, failedLogSearchPiece(result.Chunk, result.Err))
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
			pieces = append(pieces, failedLogSearchPiece(result.Chunk, err))
			continue
		}
		succeeded++
		items = append(items, windowItems...)
		if windowTotal, ok := result.Value["total_matching_lines"].(int); ok {
			total += windowTotal
			hasTotal = true
		}
		windowTruncated, _ := result.Value["logs_truncated"].(bool)
		truncated = truncated || windowTruncated
		entries := countLogEntriesInResultItems(windowItems)
		status := logSearchPieceComplete
		// The merged limit only cuts this piece's coverage if it drops one of its rows.
		limitDropsRows := limit > 0 && entries > 0 && entriesBefore+entries > limit
		if windowTruncated || limitDropsRows {
			status = logSearchPiecePartial
		}
		entriesBefore += entries
		pieces = append(pieces, logSearchPiece(result.Chunk, status))
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
	stats["pieces"] = pieces
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

func logSearchPiece(chunk utils.TimeChunk, status string) map[string]any {
	return map[string]any{
		"start":  time.UnixMilli(chunk.StartMs).UTC().Format(time.RFC3339Nano),
		"end":    time.UnixMilli(chunk.EndMs).UTC().Format(time.RFC3339Nano),
		"status": status,
	}
}

func failedLogSearchPiece(chunk utils.TimeChunk, err error) map[string]any {
	piece := logSearchPiece(chunk, logSearchPieceFailed)
	piece["error"] = err.Error()
	return piece
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
