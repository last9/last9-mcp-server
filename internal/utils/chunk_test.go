package utils

import (
	"testing"
	"time"
)

func TestGetTimeRangeChunksBackward_SubChunk(t *testing.T) {
	start := int64(0)
	end := (3 * time.Minute).Milliseconds()

	chunks := GetTimeRangeChunksBackward(start, end)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0].StartMs != start || chunks[0].EndMs != end {
		t.Fatalf("unexpected chunk bounds: %#v", chunks[0])
	}
}

func TestGetTimeRangeChunksBackward_ExactChunkSize(t *testing.T) {
	start := int64(0)
	end := (30 * time.Minute).Milliseconds()

	chunks := GetTimeRangeChunksBackward(start, end)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0].StartMs != start || chunks[0].EndMs != end {
		t.Fatalf("unexpected chunk bounds: %#v", chunks[0])
	}
}

func TestGetTimeRangeChunksBackward_MultipleChunks(t *testing.T) {
	start := int64(0)
	end := (2 * time.Hour).Milliseconds()

	chunks := GetTimeRangeChunksBackward(start, end)
	if len(chunks) != 4 {
		t.Fatalf("expected 4 chunks, got %d", len(chunks))
	}

	expected := []TimeChunk{
		{StartMs: (90 * time.Minute).Milliseconds(), EndMs: end},
		{StartMs: (60 * time.Minute).Milliseconds(), EndMs: (90 * time.Minute).Milliseconds()},
		{StartMs: (30 * time.Minute).Milliseconds(), EndMs: (60 * time.Minute).Milliseconds()},
		{StartMs: start, EndMs: (30 * time.Minute).Milliseconds()},
	}

	for i, want := range expected {
		if chunks[i] != want {
			t.Fatalf("chunk %d mismatch: got %#v want %#v", i, chunks[i], want)
		}
	}
}

func TestGetTimeRangeChunksBackward_UnevenRangeIsContiguous(t *testing.T) {
	start := int64(0)
	end := (40 * time.Minute).Milliseconds()

	chunks := GetTimeRangeChunksBackward(start, end)
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}
	if chunks[0].StartMs != chunks[1].EndMs {
		t.Fatalf("expected contiguous chunks, got %#v", chunks)
	}
	if chunks[0].EndMs != end || chunks[1].StartMs != start {
		t.Fatalf("expected full range coverage, got %#v", chunks)
	}
}

func TestGetTimeRangeChunksBackward_EmptyRange(t *testing.T) {
	if chunks := GetTimeRangeChunksBackward(100, 100); chunks != nil {
		t.Fatalf("expected nil for equal start/end, got %#v", chunks)
	}
	if chunks := GetTimeRangeChunksBackward(200, 100); chunks != nil {
		t.Fatalf("expected nil for inverted range, got %#v", chunks)
	}
}
