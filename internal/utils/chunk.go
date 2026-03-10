package utils

import "time"

// TimeChunk represents a single time range chunk with start/end in milliseconds.
type TimeChunk struct {
	StartMs int64
	EndMs   int64
}

// Chunking thresholds — adapted from the dashboard's getTimeRangeChunks logic.
// Chunk sizes increase with the total time range to balance API load vs. result coverage.
const (
	chunkSize5Min  = 5 * time.Minute
	chunkSize15Min = 15 * time.Minute
	chunkSize1Hour = 1 * time.Hour

	threshold6Hours = 6 * time.Hour
	threshold24Hours = 24 * time.Hour

	// ChunkThreshold is the minimum duration before chunking kicks in.
	// Below this, a single API call is sufficient.
	ChunkThreshold = 5 * time.Minute
)

// chunkSizeForDuration returns the chunk duration based on the total time range,
// mirroring the dashboard's adaptive approach.
func chunkSizeForDuration(total time.Duration) time.Duration {
	switch {
	case total <= threshold6Hours:
		return chunkSize5Min
	case total <= threshold24Hours:
		return chunkSize15Min
	default:
		return chunkSize1Hour
	}
}

// GetTimeRangeChunksBackward splits a time range (in milliseconds) into chunks
// starting from the end, walking backward. This matches how the dashboard's
// getTimeRangeChunks works — most-recent-first ordering for backward log queries.
func GetTimeRangeChunksBackward(startMs, endMs int64) []TimeChunk {
	totalDuration := time.Duration(endMs-startMs) * time.Millisecond

	// No chunking needed for small ranges.
	if totalDuration <= ChunkThreshold {
		return []TimeChunk{{StartMs: startMs, EndMs: endMs}}
	}

	chunkMs := chunkSizeForDuration(totalDuration).Milliseconds()
	var chunks []TimeChunk

	currentEnd := endMs
	for currentEnd > startMs {
		chunkStart := currentEnd - chunkMs
		if chunkStart < startMs {
			chunkStart = startMs
		}
		chunks = append(chunks, TimeChunk{StartMs: chunkStart, EndMs: currentEnd})
		currentEnd = chunkStart
	}

	return chunks
}
