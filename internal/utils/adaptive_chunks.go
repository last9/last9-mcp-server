package utils

import (
	"fmt"
	"time"
)

// Port of dashboard/app/src/App/scenes/Logs/adaptive-chunk-loader.ts and the
// adaptive sizing logic from scenes/Logs/utils.tsx (getVolumeQueryChunks).
//
// All knobs are at module scope and named to mirror the frontend so a reviewer
// can diff the two implementations rule-for-rule. Per-org overrides (the
// `orgProperties.volume_parallel_chunks_*` keys on the frontend) are
// intentionally not ported — the MCP server has no settings fetcher today.

// ParallelCallsLimit is the hard ceiling on parallel chunk requests for a
// single tool call. Mirrors frontend PARALLEL_CALLS_LIMIT (6).
const ParallelCallsLimit = 6

// Default parallel-chunk counts mirroring DEFAULT_* constants in
// adaptive-chunk-loader.ts:20-26.
const (
	DefaultJSONParseOptimizedCount = 1
	DefaultBodyParseOptimizedCount = 2
	DefaultGt14dCount              = 4
	DefaultGt7dCount               = 3
	DefaultGt2dCount               = 2
	DefaultBodyGt1dCount           = 2
	// DefaultCount mirrors the frontend's DEFAULT_COUNT = Infinity — i.e.
	// "fire as many chunks in parallel as the cap allows".
	DefaultCount = ParallelCallsLimit
)

// AdaptiveLoadingConfig describes the resolved parallelism + chunk-sizing
// policy for a single tool call. Mirrors frontend AdaptiveLoadingConfig in
// adaptive-chunk-loader.ts:40.
type AdaptiveLoadingConfig struct {
	MaxParallelChunks      int
	ChunkSizeMs            int64
	TimeRangeMs            int64
	HasExpensiveBodySearch bool
	HasJSONParsePipeline   bool
	Reason                 string
}

// AdaptiveLoadingInput is the structured argument to GetAdaptiveLoadingConfig.
// Mirrors the destructured arg in frontend getAdaptiveLoadingConfig.
type AdaptiveLoadingInput struct {
	StartMs                       int64
	EndMs                         int64
	Pipeline                      []map[string]any
	ShouldOptimizeLineFilterQuery bool
}

// Time-range thresholds (milliseconds) — mirror frontend ONE_DAY / TWO_DAYS /
// SEVEN_DAYS / 14d constants from adaptive-chunk-loader.ts:11-12 and the
// `ONE_DAY` import.
const (
	OneHourMs      int64 = int64(time.Hour / time.Millisecond)
	OneDayMs       int64 = 24 * OneHourMs
	TwoDaysMs      int64 = 2 * OneDayMs
	SevenDaysMs    int64 = 7 * OneDayMs
	FourteenDaysMs int64 = 14 * OneDayMs
)

// Adaptive-sizing knobs. Mirror getVolumeQueryChunks parameters with sensible
// MCP-side defaults (the dashboard derives these from query mode + window).
const (
	// SplitThresholdMs: ranges at or below this size are not split into
	// multiple chunks. Matches the frontend's typical small-range short-circuit.
	SplitThresholdMs int64 = OneHourMs
	// MaxChunkSizeMs caps the size of any single chunk. Matches the frontend's
	// "1-hour chunks for body-parse > 1h" upper bound.
	MaxChunkSizeMs int64 = OneHourMs
	// IdealChunksPerQuery mirrors the frontend's "range / 6" rule when the
	// query is not in the body-parse-1h fast path.
	IdealChunksPerQuery int64 = 6
)

// getParallelChunksCount mirrors the frontend helper of the same name in
// adaptive-chunk-loader.ts:55 — caps the configured count at the hard ceiling.
func getParallelChunksCount(count int) int {
	if count > ParallelCallsLimit {
		return ParallelCallsLimit
	}
	if count < 1 {
		return 1
	}
	return count
}

// GetAdaptiveLoadingConfig picks the optimal parallelism + chunk-size policy
// for the given query characteristics. Rules mirror frontend
// getAdaptiveLoadingConfig (adaptive-chunk-loader.ts:61-179) priority order:
//
//  0. Body search + line-filter optimization (+ JSON parse): aggressively
//     throttle — 1 or 2 chunks at a time.
//  1. Body search with time range > 1 day: 2 chunks at a time.
//  2. Time range > 14 days: 4 chunks at a time.
//  3. Time range > 7 days: 3 chunks at a time.
//  4. Time range > 2 days: 2 chunks at a time.
//  5. Default (small/medium range): fire all chunks at once, capped at
//     ParallelCallsLimit.
//
// org-property overrides exist on the frontend; this port uses only the
// DEFAULT_* values.
func GetAdaptiveLoadingConfig(in AdaptiveLoadingInput) AdaptiveLoadingConfig {
	timeRangeMs := absInt64(in.EndMs - in.StartMs)
	hasExpensiveBodySearch := HasExpensiveBodyParsing(in.Pipeline)
	hasJSONParseStage := HasJSONParsePipeline(in.Pipeline)

	chunkSize := getAdaptiveChunkSizeMs(timeRangeMs, hasExpensiveBodySearch, in.ShouldOptimizeLineFilterQuery)

	// Rule 0: line-filter optimization is on AND body search is expensive.
	if hasExpensiveBodySearch && in.ShouldOptimizeLineFilterQuery {
		if hasJSONParseStage {
			return AdaptiveLoadingConfig{
				MaxParallelChunks:      getParallelChunksCount(DefaultJSONParseOptimizedCount),
				ChunkSizeMs:            chunkSize,
				TimeRangeMs:            timeRangeMs,
				HasExpensiveBodySearch: hasExpensiveBodySearch,
				HasJSONParsePipeline:   hasJSONParseStage,
				Reason: fmt.Sprintf("Body search with line filter optimization and JSON parsing enabled - processing %d chunk(s) at a time",
					DefaultJSONParseOptimizedCount),
			}
		}
		return AdaptiveLoadingConfig{
			MaxParallelChunks:      getParallelChunksCount(DefaultBodyParseOptimizedCount),
			ChunkSizeMs:            chunkSize,
			TimeRangeMs:            timeRangeMs,
			HasExpensiveBodySearch: hasExpensiveBodySearch,
			HasJSONParsePipeline:   hasJSONParseStage,
			Reason: fmt.Sprintf("Body search with line filter optimization enabled - processing %d chunk(s) at a time",
				DefaultBodyParseOptimizedCount),
		}
	}

	// Rule 1: body search with > 1 day range.
	if hasExpensiveBodySearch && timeRangeMs > OneDayMs {
		return AdaptiveLoadingConfig{
			MaxParallelChunks:      getParallelChunksCount(DefaultBodyGt1dCount),
			ChunkSizeMs:            chunkSize,
			TimeRangeMs:            timeRangeMs,
			HasExpensiveBodySearch: hasExpensiveBodySearch,
			HasJSONParsePipeline:   hasJSONParseStage,
			Reason: fmt.Sprintf("Body search with time range > 1 day (%dd) - processing %d chunk(s) at a time",
				timeRangeMs/OneDayMs, DefaultBodyGt1dCount),
		}
	}

	// Rule 2: range > 14 days.
	if timeRangeMs > FourteenDaysMs {
		return AdaptiveLoadingConfig{
			MaxParallelChunks:      getParallelChunksCount(DefaultGt14dCount),
			ChunkSizeMs:            chunkSize,
			TimeRangeMs:            timeRangeMs,
			HasExpensiveBodySearch: hasExpensiveBodySearch,
			HasJSONParsePipeline:   hasJSONParseStage,
			Reason: fmt.Sprintf("Very large time range > 14 days (%dd) - processing %d chunk(s) at a time",
				timeRangeMs/OneDayMs, DefaultGt14dCount),
		}
	}

	// Rule 3: range > 7 days.
	if timeRangeMs > SevenDaysMs {
		return AdaptiveLoadingConfig{
			MaxParallelChunks:      getParallelChunksCount(DefaultGt7dCount),
			ChunkSizeMs:            chunkSize,
			TimeRangeMs:            timeRangeMs,
			HasExpensiveBodySearch: hasExpensiveBodySearch,
			HasJSONParsePipeline:   hasJSONParseStage,
			Reason: fmt.Sprintf("Large time range > 7 days (%dd) - processing %d chunk(s) at a time",
				timeRangeMs/OneDayMs, DefaultGt7dCount),
		}
	}

	// Rule 4: range > 2 days.
	if timeRangeMs > TwoDaysMs {
		return AdaptiveLoadingConfig{
			MaxParallelChunks:      getParallelChunksCount(DefaultGt2dCount),
			ChunkSizeMs:            chunkSize,
			TimeRangeMs:            timeRangeMs,
			HasExpensiveBodySearch: hasExpensiveBodySearch,
			HasJSONParsePipeline:   hasJSONParseStage,
			Reason: fmt.Sprintf("Time range > 2 days (%dd) - processing %d chunk(s) at a time",
				timeRangeMs/OneDayMs, DefaultGt2dCount),
		}
	}

	// Default: small/medium range.
	return AdaptiveLoadingConfig{
		MaxParallelChunks:      getParallelChunksCount(DefaultCount),
		ChunkSizeMs:            chunkSize,
		TimeRangeMs:            timeRangeMs,
		HasExpensiveBodySearch: hasExpensiveBodySearch,
		HasJSONParsePipeline:   hasJSONParseStage,
		Reason:                 "Small time range - processing all chunks simultaneously",
	}
}

// getAdaptiveChunkSizeMs mirrors the size-decision branch inside frontend
// getVolumeQueryChunks (utils.tsx:1343).
//
//   - Range ≤ SplitThresholdMs: caller can keep the whole range as one chunk
//     (we still return SplitThresholdMs to keep callers branch-free).
//   - Body-parse over 1 hour with line-filter optimization: fixed 1-hour
//     chunks ("shouldSplitIntoOneHourChunks" in the frontend).
//   - Otherwise: range / 6, capped at MaxChunkSizeMs.
func getAdaptiveChunkSizeMs(timeRangeMs int64, hasExpensiveBodySearch, shouldOptimize bool) int64 {
	if timeRangeMs <= SplitThresholdMs {
		// Use the full range as the chunk size for sub-threshold queries.
		// The empty-range branch in GetAdaptiveChunks short-circuits to nil,
		// so we never emit a zero-size chunk; the SplitThresholdMs fallback
		// here is just defensive in case a caller invokes this helper directly.
		if timeRangeMs <= 0 {
			return SplitThresholdMs
		}
		return timeRangeMs
	}

	if shouldOptimize && hasExpensiveBodySearch && timeRangeMs > OneHourMs {
		return OneHourMs
	}

	// Ceiling division — matches frontend Math.ceil(queryTimeDuration / 6).
	// Floor would emit an extra short tail chunk for non-divisible ranges.
	ideal := (timeRangeMs + IdealChunksPerQuery - 1) / IdealChunksPerQuery
	if ideal > MaxChunkSizeMs {
		return MaxChunkSizeMs
	}
	if ideal < 1 {
		return 1
	}
	return ideal
}

// GetAdaptiveChunks splits [startMs, endMs] into time chunks using the size
// produced by GetAdaptiveLoadingConfig. Newest-first ordering, contiguous,
// no overlap. Equivalent to the frontend's getVolumeQueryChunks but without
// the volume-chart window alignment (we don't render volume bars here).
func GetAdaptiveChunks(startMs, endMs int64, cfg AdaptiveLoadingConfig) []TimeChunk {
	if endMs <= startMs {
		return nil
	}
	if endMs-startMs <= SplitThresholdMs {
		return []TimeChunk{{StartMs: startMs, EndMs: endMs}}
	}
	return splitRangeBackward(startMs, endMs, cfg.ChunkSizeMs)
}

// splitRangeBackward emits chunks of chunkSizeMs newest-first across the
// half-open interval (startMs, endMs]. The final chunk may be shorter than
// chunkSizeMs to absorb the remainder. Mirrors the backward loop in
// adaptive-chunk-loader's caller and frontend getTimeRangeChunks.
func splitRangeBackward(startMs, endMs, chunkSizeMs int64) []TimeChunk {
	if chunkSizeMs <= 0 {
		chunkSizeMs = SplitThresholdMs
	}
	chunks := make([]TimeChunk, 0)
	currentEnd := endMs
	for currentEnd > startMs {
		chunkStart := currentEnd - chunkSizeMs
		if chunkStart < startMs {
			chunkStart = startMs
		}
		chunks = append(chunks, TimeChunk{StartMs: chunkStart, EndMs: currentEnd})
		currentEnd = chunkStart
	}
	return chunks
}

func absInt64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}
