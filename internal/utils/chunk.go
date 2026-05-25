package utils

import "time"

const logChunkSize = 30 * time.Minute

// TimeChunk represents a contiguous time slice in milliseconds.
type TimeChunk struct {
	StartMs int64
	EndMs   int64
}

// GetTimeRangeChunksBackward splits a range into contiguous chunks of
// logChunkSize, ordered from newest to oldest.
func GetTimeRangeChunksBackward(startMs, endMs int64) []TimeChunk {
	if endMs <= startMs {
		return nil
	}

	if endMs-startMs <= logChunkSize.Milliseconds() {
		return []TimeChunk{{StartMs: startMs, EndMs: endMs}}
	}

	chunks := make([]TimeChunk, 0)
	currentEnd := endMs
	chunkSizeMs := logChunkSize.Milliseconds()

	for currentEnd > startMs {
		chunkStart := currentEnd - chunkSizeMs
		if chunkStart < startMs {
			chunkStart = startMs
		}

		chunks = append(chunks, TimeChunk{
			StartMs: chunkStart,
			EndMs:   currentEnd,
		})

		currentEnd = chunkStart
	}

	return chunks
}
