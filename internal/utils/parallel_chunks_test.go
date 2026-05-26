package utils

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunChunksParallelPreservesOrder(t *testing.T) {
	chunks := []TimeChunk{
		{StartMs: 0, EndMs: 100},
		{StartMs: 100, EndMs: 200},
		{StartMs: 200, EndMs: 300},
		{StartMs: 300, EndMs: 400},
	}

	// fn returns sleep based on index — later indices finish first, but the
	// returned slice must be ordered by Index.
	results := RunChunksParallel(context.Background(), chunks, 4,
		func(_ context.Context, idx int, chunk TimeChunk) (int, error) {
			time.Sleep(time.Duration(len(chunks)-idx) * 10 * time.Millisecond)
			return idx, nil
		})

	if len(results) != len(chunks) {
		t.Fatalf("expected %d results, got %d", len(chunks), len(results))
	}
	for i, r := range results {
		if r.Index != i {
			t.Fatalf("result %d has Index=%d (want %d)", i, r.Index, i)
		}
		if r.Value != i {
			t.Fatalf("result %d Value=%d (want %d)", i, r.Value, i)
		}
		if r.Chunk != chunks[i] {
			t.Fatalf("result %d Chunk mismatch: got %#v want %#v", i, r.Chunk, chunks[i])
		}
	}
}

func TestRunChunksParallelCapturesPerChunkError(t *testing.T) {
	chunks := []TimeChunk{
		{StartMs: 0, EndMs: 100},
		{StartMs: 100, EndMs: 200},
		{StartMs: 200, EndMs: 300},
	}
	bad := errors.New("chunk 1 boom")

	results := RunChunksParallel(context.Background(), chunks, 3,
		func(_ context.Context, idx int, _ TimeChunk) (int, error) {
			if idx == 1 {
				return 0, bad
			}
			return idx, nil
		})

	if results[0].Err != nil || results[2].Err != nil {
		t.Fatalf("expected successful sibling chunks, got %#v / %#v", results[0], results[2])
	}
	if !errors.Is(results[1].Err, bad) {
		t.Fatalf("expected chunk 1 error to be captured, got %#v", results[1])
	}
}

func TestRunChunksParallelRespectsConcurrencyCap(t *testing.T) {
	chunks := make([]TimeChunk, 20)
	var (
		inflight    atomic.Int32
		maxObserved atomic.Int32
	)

	results := RunChunksParallel(context.Background(), chunks, 5,
		func(_ context.Context, _ int, _ TimeChunk) (int, error) {
			cur := inflight.Add(1)
			for {
				prev := maxObserved.Load()
				if cur <= prev || maxObserved.CompareAndSwap(prev, cur) {
					break
				}
			}
			time.Sleep(10 * time.Millisecond)
			inflight.Add(-1)
			return 0, nil
		})

	if len(results) != len(chunks) {
		t.Fatalf("expected %d results, got %d", len(chunks), len(results))
	}
	if got := maxObserved.Load(); got > 5 {
		t.Fatalf("expected at most 5 concurrent goroutines, observed %d", got)
	}
	if got := maxObserved.Load(); got < 2 {
		t.Fatalf("expected concurrency > 1 to validate parallelism, observed %d", got)
	}
}

func TestRunChunksParallelEmptyInputReturnsNil(t *testing.T) {
	results := RunChunksParallel(context.Background(), nil, 5,
		func(_ context.Context, _ int, _ TimeChunk) (int, error) { return 0, nil })
	if results != nil {
		t.Fatalf("expected nil results for empty chunks, got %#v", results)
	}
}

func TestRunChunksParallelCancelsQueuedChunksOnContextCancel(t *testing.T) {
	chunks := make([]TimeChunk, 10) // many more than maxConcurrency=2
	started := make(chan struct{}, 10)
	release := make(chan struct{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		// Wait until the first batch of 2 chunks has started, then cancel.
		<-started
		<-started
		cancel()
		close(release)
	}()

	results := RunChunksParallel(ctx, chunks, 2,
		func(c context.Context, _ int, _ TimeChunk) (int, error) {
			started <- struct{}{}
			<-release
			return 0, c.Err()
		})

	cancelled := 0
	for _, r := range results {
		if errors.Is(r.Err, context.Canceled) {
			cancelled++
		}
	}
	// At minimum, the 8 queued chunks should observe Canceled without ever
	// being admitted past the semaphore.
	if cancelled < 8 {
		t.Fatalf("expected >=8 chunks to report context.Canceled, got %d (results=%#v)", cancelled, results)
	}
}

func TestRunChunksParallelHonorsPerCallContextTimeout(t *testing.T) {
	chunks := []TimeChunk{{StartMs: 0, EndMs: 100}}

	results := RunChunksParallel(context.Background(), chunks, 1,
		func(ctx context.Context, _ int, _ TimeChunk) (int, error) {
			deadlineCtx, cancel := context.WithTimeout(ctx, 5*time.Millisecond)
			defer cancel()
			select {
			case <-deadlineCtx.Done():
				return 0, deadlineCtx.Err()
			case <-time.After(50 * time.Millisecond):
				return 0, nil
			}
		})

	if results[0].Err == nil {
		t.Fatal("expected context deadline error from chunk fn")
	}
}
