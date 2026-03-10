package utils

import (
	"testing"
	"time"
)

func TestGetTimeRangeChunksBackward_SmallRange(t *testing.T) {
	// 3 minutes — below ChunkThreshold, should return a single chunk.
	start := int64(1000000)
	end := start + (3 * time.Minute).Milliseconds()

	chunks := GetTimeRangeChunksBackward(start, end)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0].StartMs != start || chunks[0].EndMs != end {
		t.Fatalf("expected chunk [%d, %d], got [%d, %d]", start, end, chunks[0].StartMs, chunks[0].EndMs)
	}
}

func TestGetTimeRangeChunksBackward_OneHour(t *testing.T) {
	// 1 hour = 60 min, chunk size = 5 min → 12 chunks
	start := int64(0)
	end := (1 * time.Hour).Milliseconds()

	chunks := GetTimeRangeChunksBackward(start, end)
	if len(chunks) != 12 {
		t.Fatalf("expected 12 chunks for 1h range with 5min chunks, got %d", len(chunks))
	}

	// First chunk should cover the most recent 5 minutes.
	if chunks[0].EndMs != end {
		t.Fatalf("first chunk should end at %d, got %d", end, chunks[0].EndMs)
	}
	if chunks[0].StartMs != end-(5*time.Minute).Milliseconds() {
		t.Fatalf("first chunk start mismatch")
	}

	// Last chunk should start at 0.
	if chunks[len(chunks)-1].StartMs != start {
		t.Fatalf("last chunk should start at %d, got %d", start, chunks[len(chunks)-1].StartMs)
	}
}

func TestGetTimeRangeChunksBackward_TenHours(t *testing.T) {
	// 10 hours → chunk size = 15 min → 40 chunks
	start := int64(0)
	end := (10 * time.Hour).Milliseconds()

	chunks := GetTimeRangeChunksBackward(start, end)
	expected := 40
	if len(chunks) != expected {
		t.Fatalf("expected %d chunks for 10h range with 15min chunks, got %d", expected, len(chunks))
	}
}

func TestGetTimeRangeChunksBackward_ThreeDays(t *testing.T) {
	// 72 hours → chunk size = 1 hour → 72 chunks
	start := int64(0)
	end := (72 * time.Hour).Milliseconds()

	chunks := GetTimeRangeChunksBackward(start, end)
	if len(chunks) != 72 {
		t.Fatalf("expected 72 chunks for 72h range with 1h chunks, got %d", len(chunks))
	}
}

func TestGetTimeRangeChunksBackward_Contiguous(t *testing.T) {
	// Verify chunks are contiguous and non-overlapping.
	start := int64(0)
	end := (2 * time.Hour).Milliseconds()

	chunks := GetTimeRangeChunksBackward(start, end)

	for i := 0; i < len(chunks)-1; i++ {
		if chunks[i].StartMs != chunks[i+1].EndMs {
			t.Fatalf("gap between chunk %d (start=%d) and chunk %d (end=%d)",
				i, chunks[i].StartMs, i+1, chunks[i+1].EndMs)
		}
	}

	// First chunk ends at total end, last chunk starts at total start.
	if chunks[0].EndMs != end {
		t.Fatalf("first chunk should end at %d", end)
	}
	if chunks[len(chunks)-1].StartMs != start {
		t.Fatalf("last chunk should start at %d", start)
	}
}

func TestGetTimeRangeChunksBackward_UnevenDivision(t *testing.T) {
	// 7 minutes with 5-min chunks → 2 chunks: [2min, 5min] and [0, 2min]
	start := int64(0)
	end := (7 * time.Minute).Milliseconds()

	chunks := GetTimeRangeChunksBackward(start, end)
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}

	// First chunk: [2min, 7min]
	if chunks[0].EndMs != end {
		t.Fatalf("first chunk should end at %d", end)
	}
	if chunks[0].StartMs != (2 * time.Minute).Milliseconds() {
		t.Fatalf("first chunk start mismatch: got %d", chunks[0].StartMs)
	}

	// Second chunk: [0, 2min]
	if chunks[1].StartMs != start {
		t.Fatalf("second chunk should start at %d", start)
	}
	if chunks[1].EndMs != (2 * time.Minute).Milliseconds() {
		t.Fatalf("second chunk end mismatch: got %d", chunks[1].EndMs)
	}
}
