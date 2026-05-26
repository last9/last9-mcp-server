package utils

// TimeChunk represents a contiguous time slice in milliseconds. It's the
// common currency between the adaptive chunk splitter (GetAdaptiveChunks)
// and the parallel executor (RunChunksParallel).
type TimeChunk struct {
	StartMs int64
	EndMs   int64
}
