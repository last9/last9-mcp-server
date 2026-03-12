package utils

import (
	"testing"
	"time"
)

func TestGetTimeRangeChunksBackward_SubFiveMinutes(t *testing.T) {
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

func TestGetTimeRangeChunksBackward_ExactFiveMinutes(t *testing.T) {
	start := int64(0)
	end := (5 * time.Minute).Milliseconds()

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
	end := (12 * time.Minute).Milliseconds()

	chunks := GetTimeRangeChunksBackward(start, end)
	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks, got %d", len(chunks))
	}

	expected := []TimeChunk{
		{StartMs: (7 * time.Minute).Milliseconds(), EndMs: end},
		{StartMs: (2 * time.Minute).Milliseconds(), EndMs: (7 * time.Minute).Milliseconds()},
		{StartMs: start, EndMs: (2 * time.Minute).Milliseconds()},
	}

	for i, want := range expected {
		if chunks[i] != want {
			t.Fatalf("chunk %d mismatch: got %#v want %#v", i, chunks[i], want)
		}
	}
}

func TestGetTimeRangeChunksBackward_UnevenRangeIsContiguous(t *testing.T) {
	start := int64(0)
	end := (7 * time.Minute).Milliseconds()

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
