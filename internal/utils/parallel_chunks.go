package utils

import (
	"context"
	"sync"
)

// ChunkResult is the per-chunk outcome of RunChunksParallel. Index matches
// the position of the chunk in the input slice so callers can walk results
// in original (newest-first) order regardless of completion order.
type ChunkResult[T any] struct {
	Index int
	Chunk TimeChunk
	Value T
	Err   error
}

// RunChunksParallel executes fn for every chunk with a fixed-size semaphore
// limiting concurrency to maxConcurrency. Results are returned in input order
// (each goroutine writes to its own index in the pre-sized slice, so no shared
// mutation is needed). Per-chunk errors are captured in the ChunkResult rather
// than short-circuiting; callers decide how to handle partial failure.
//
// Cancellation: queued goroutines waiting for a semaphore slot bail out
// immediately when ctx is cancelled, recording ctx.Err() against their chunk;
// in-flight chunks observe the same ctx inside fn and are expected to honour it.
func RunChunksParallel[T any](
	ctx context.Context,
	chunks []TimeChunk,
	maxConcurrency int,
	fn func(ctx context.Context, idx int, chunk TimeChunk) (T, error),
) []ChunkResult[T] {
	if len(chunks) == 0 {
		return nil
	}
	if maxConcurrency < 1 {
		maxConcurrency = 1
	}

	results := make([]ChunkResult[T], len(chunks))
	sem := make(chan struct{}, maxConcurrency)
	var wg sync.WaitGroup

	for i, chunk := range chunks {
		wg.Add(1)
		go func(i int, chunk TimeChunk) {
			defer wg.Done()

			// Acquire a slot, but exit early if the caller has cancelled.
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				results[i] = ChunkResult[T]{Index: i, Chunk: chunk, Err: ctx.Err()}
				return
			}
			defer func() { <-sem }()

			value, err := fn(ctx, i, chunk)
			results[i] = ChunkResult[T]{
				Index: i,
				Chunk: chunk,
				Value: value,
				Err:   err,
			}
		}(i, chunk)
	}

	wg.Wait()
	return results
}
