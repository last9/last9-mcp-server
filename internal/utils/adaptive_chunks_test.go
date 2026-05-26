package utils

import "testing"

// Tests mirror frontend adaptive-chunk-loader.test.ts cases so behaviour
// parity is easy to audit rule-for-rule.

func TestHasJSONParsePipeline(t *testing.T) {
	if HasJSONParsePipeline(nil) {
		t.Fatal("nil pipeline should not be JSON-parsing")
	}

	pipe := []map[string]any{
		{"type": "filter", "query": map[string]any{"$eq": []any{"ServiceName", "api"}}},
		{"type": "parse", "parser": "json", "field": "Body"},
	}
	if !HasJSONParsePipeline(pipe) {
		t.Fatal("expected JSON parse to be detected")
	}

	nonJSON := []map[string]any{
		{"type": "parse", "parser": "regexp", "field": "Body"},
	}
	if HasJSONParsePipeline(nonJSON) {
		t.Fatal("regexp parse should not be flagged as JSON parse")
	}
}

func TestHasBodyParseOrTransform_DetectsBodyParse(t *testing.T) {
	pipe := []map[string]any{
		{"type": "parse", "parser": "logfmt", "field": "Body"},
	}
	if !HasBodyParseOrTransform(pipe) {
		t.Fatal("expected body parse to be detected")
	}
}

func TestHasBodyParseOrTransform_DetectsBodyTransform(t *testing.T) {
	// $split_into is in BODY_TRANSFORM_OPERATIONS; "Body" as first arg = body work.
	pipe := []map[string]any{
		{"type": "transform", "transforms": []any{
			map[string]any{"function": map[string]any{"$split_into": []any{"Body", "msg"}}},
		}},
	}
	if !HasBodyParseOrTransform(pipe) {
		t.Fatal("expected body transform to be detected")
	}
}

func TestHasBodyParseOrTransform_IgnoresUnlistedTransform(t *testing.T) {
	// $upper is NOT in BODY_TRANSFORM_OPERATIONS — frontend ignores it, so do we.
	pipe := []map[string]any{
		{"type": "transform", "transforms": []any{
			map[string]any{"function": map[string]any{"$upper": []any{"Body"}}},
		}},
	}
	if HasBodyParseOrTransform(pipe) {
		t.Fatal("transform op outside BODY_TRANSFORM_OPERATIONS should be ignored")
	}
}

func TestHasBodyParseOrTransform_IgnoresNonBodyField(t *testing.T) {
	pipe := []map[string]any{
		{"type": "parse", "parser": "json", "field": "Attributes"},
		{"type": "transform", "transforms": []any{
			map[string]any{"function": map[string]any{"$split": []any{"ServiceName", ","}}},
		}},
	}
	if HasBodyParseOrTransform(pipe) {
		t.Fatal("non-Body parse/transform should not be flagged")
	}
}

func TestHasExpensiveBodyParsing_ParseTrumpsEverything(t *testing.T) {
	pipe := []map[string]any{
		{"type": "parse", "parser": "json", "field": "Body"},
	}
	if !HasExpensiveBodyParsing(pipe) {
		t.Fatal("body parse should always be expensive")
	}
}

func TestHasExpensiveBodyParsing_OptimizedEqualityIsCheap(t *testing.T) {
	pipe := []map[string]any{
		{"type": "filter", "query": map[string]any{
			"$eq": []any{"Body", "exact-match"},
		}},
	}
	if HasExpensiveBodyParsing(pipe) {
		t.Fatal("$eq on Body should be considered optimized (indexable)")
	}
}

func TestHasExpensiveBodyParsing_ContainsIsExpensive(t *testing.T) {
	pipe := []map[string]any{
		{"type": "filter", "query": map[string]any{
			"$contains": []any{"Body", "timeout"},
		}},
	}
	if !HasExpensiveBodyParsing(pipe) {
		t.Fatal("$contains on Body should be expensive (non-indexable)")
	}
}

func TestHasExpensiveBodyParsing_AnyOptimizedStageWins(t *testing.T) {
	// Mirrors frontend behaviour: if ANY stage's body filter is optimized,
	// the whole pipeline is treated as not-expensive.
	pipe := []map[string]any{
		{"type": "filter", "query": map[string]any{
			"$contains": []any{"Body", "noise"},
		}},
		{"type": "filter", "query": map[string]any{
			"$eq": []any{"Body", "exact"},
		}},
	}
	if HasExpensiveBodyParsing(pipe) {
		t.Fatal("optimized stage should short-circuit expensive detection")
	}
}

func TestHasExpensiveBodyParsing_MixedOperatorsWithinStageIsOptimized(t *testing.T) {
	// Single filter stage mixing $contains and $eq on Body. Frontend's
	// isFilterStageOptimized treats the stage as optimized if ANY operator
	// group is bloom-eligible — so $eq presence makes the whole stage cheap.
	pipe := []map[string]any{
		{"type": "filter", "query": map[string]any{
			"$and": []any{
				map[string]any{"$contains": []any{"Body", "noise"}},
				map[string]any{"$eq": []any{"Body", "exact"}},
			},
		}},
	}
	if HasExpensiveBodyParsing(pipe) {
		t.Fatal("mixed indexable+non-indexable Body ops within a stage should still mark the stage optimized")
	}
}

func TestHasExpensiveBodyParsing_OnlyNonIndexableOperatorsIsExpensive(t *testing.T) {
	pipe := []map[string]any{
		{"type": "filter", "query": map[string]any{
			"$and": []any{
				map[string]any{"$contains": []any{"Body", "x"}},
				map[string]any{"$matches": []any{"Body", "y.*z"}},
			},
		}},
	}
	if !HasExpensiveBodyParsing(pipe) {
		t.Fatal("stage with no indexable Body operator should be expensive")
	}
}

func TestHasExpensiveBodyParsing_NoBodyFilterAtAll(t *testing.T) {
	pipe := []map[string]any{
		{"type": "filter", "query": map[string]any{
			"$eq": []any{"ServiceName", "api"},
		}},
	}
	if HasExpensiveBodyParsing(pipe) {
		t.Fatal("pipeline without any Body reference is not expensive")
	}
}

func TestHasExpensiveBodyParsing_NestedConditionTree(t *testing.T) {
	pipe := []map[string]any{
		{"type": "filter", "query": map[string]any{
			"$and": []any{
				map[string]any{"$eq": []any{"ServiceName", "api"}},
				map[string]any{"$or": []any{
					map[string]any{"$contains": []any{"Body", "deadline"}},
				}},
			},
		}},
	}
	if !HasExpensiveBodyParsing(pipe) {
		t.Fatal("nested $contains on Body should still be detected")
	}
}

// --- GetAdaptiveLoadingConfig rule coverage -----------------------------------

func TestGetAdaptiveLoadingConfig_DefaultSmallRange(t *testing.T) {
	cfg := GetAdaptiveLoadingConfig(AdaptiveLoadingInput{
		StartMs: 0,
		EndMs:   OneHourMs,
	})
	if cfg.MaxParallelChunks != ParallelCallsLimit {
		t.Fatalf("small range should use ParallelCallsLimit (%d), got %d", ParallelCallsLimit, cfg.MaxParallelChunks)
	}
}

func TestGetAdaptiveLoadingConfig_Gt2dRule(t *testing.T) {
	cfg := GetAdaptiveLoadingConfig(AdaptiveLoadingInput{
		StartMs: 0,
		EndMs:   3 * OneDayMs,
	})
	if cfg.MaxParallelChunks != DefaultGt2dCount {
		t.Fatalf("3-day range should use DefaultGt2dCount (%d), got %d", DefaultGt2dCount, cfg.MaxParallelChunks)
	}
}

func TestGetAdaptiveLoadingConfig_Gt7dRule(t *testing.T) {
	cfg := GetAdaptiveLoadingConfig(AdaptiveLoadingInput{
		StartMs: 0,
		EndMs:   10 * OneDayMs,
	})
	if cfg.MaxParallelChunks != DefaultGt7dCount {
		t.Fatalf("10-day range should use DefaultGt7dCount (%d), got %d", DefaultGt7dCount, cfg.MaxParallelChunks)
	}
}

func TestGetAdaptiveLoadingConfig_Gt14dRule(t *testing.T) {
	cfg := GetAdaptiveLoadingConfig(AdaptiveLoadingInput{
		StartMs: 0,
		EndMs:   20 * OneDayMs,
	})
	if cfg.MaxParallelChunks != DefaultGt14dCount {
		t.Fatalf("20-day range should use DefaultGt14dCount (%d), got %d", DefaultGt14dCount, cfg.MaxParallelChunks)
	}
}

func TestGetAdaptiveLoadingConfig_BodySearchOver1Day(t *testing.T) {
	cfg := GetAdaptiveLoadingConfig(AdaptiveLoadingInput{
		StartMs: 0,
		EndMs:   2 * OneDayMs,
		Pipeline: []map[string]any{
			{"type": "filter", "query": map[string]any{"$contains": []any{"Body", "timeout"}}},
		},
	})
	if cfg.MaxParallelChunks != DefaultBodyGt1dCount {
		t.Fatalf("body search > 1d should use DefaultBodyGt1dCount (%d), got %d", DefaultBodyGt1dCount, cfg.MaxParallelChunks)
	}
	if !cfg.HasExpensiveBodySearch {
		t.Fatal("expected HasExpensiveBodySearch=true")
	}
}

func TestGetAdaptiveLoadingConfig_LineFilterOptimizationWithJSON(t *testing.T) {
	cfg := GetAdaptiveLoadingConfig(AdaptiveLoadingInput{
		StartMs: 0,
		EndMs:   3 * OneHourMs,
		Pipeline: []map[string]any{
			{"type": "filter", "query": map[string]any{"$contains": []any{"Body", "x"}}},
			{"type": "parse", "parser": "json", "field": "Body"},
		},
		ShouldOptimizeLineFilterQuery: true,
	})
	if cfg.MaxParallelChunks != DefaultJSONParseOptimizedCount {
		t.Fatalf("expected JSON-parse-optimized count (%d), got %d", DefaultJSONParseOptimizedCount, cfg.MaxParallelChunks)
	}
}

func TestGetAdaptiveLoadingConfig_LineFilterOptimizationWithoutJSON(t *testing.T) {
	cfg := GetAdaptiveLoadingConfig(AdaptiveLoadingInput{
		StartMs: 0,
		EndMs:   3 * OneHourMs,
		Pipeline: []map[string]any{
			{"type": "filter", "query": map[string]any{"$contains": []any{"Body", "x"}}},
		},
		ShouldOptimizeLineFilterQuery: true,
	})
	if cfg.MaxParallelChunks != DefaultBodyParseOptimizedCount {
		t.Fatalf("expected body-parse-optimized count (%d), got %d", DefaultBodyParseOptimizedCount, cfg.MaxParallelChunks)
	}
}

func TestGetAdaptiveLoadingConfig_NeverExceedsParallelCallsLimit(t *testing.T) {
	cfg := GetAdaptiveLoadingConfig(AdaptiveLoadingInput{
		StartMs: 0,
		EndMs:   30 * OneDayMs,
	})
	if cfg.MaxParallelChunks > ParallelCallsLimit {
		t.Fatalf("MaxParallelChunks (%d) exceeded ParallelCallsLimit (%d)", cfg.MaxParallelChunks, ParallelCallsLimit)
	}
}

// --- GetAdaptiveChunks coverage ----------------------------------------------

func TestGetAdaptiveChunks_BelowThresholdReturnsSingleChunk(t *testing.T) {
	cfg := GetAdaptiveLoadingConfig(AdaptiveLoadingInput{StartMs: 0, EndMs: 30 * 60 * 1000})
	chunks := GetAdaptiveChunks(0, 30*60*1000, cfg)
	if len(chunks) != 1 {
		t.Fatalf("sub-threshold range should be a single chunk, got %d", len(chunks))
	}
}

func TestGetAdaptiveChunks_RangeOverSixDividesIntoSix(t *testing.T) {
	// 90-min range → range/6 = 15min chunks → 6 chunks total.
	startMs, endMs := int64(0), int64(90*60*1000)
	cfg := GetAdaptiveLoadingConfig(AdaptiveLoadingInput{StartMs: startMs, EndMs: endMs})
	chunks := GetAdaptiveChunks(startMs, endMs, cfg)
	if len(chunks) != 6 {
		t.Fatalf("expected 6 chunks (range/6), got %d", len(chunks))
	}
	for _, c := range chunks {
		if c.EndMs-c.StartMs != int64(15*60*1000) {
			t.Fatalf("expected 15-min chunk, got %dms", c.EndMs-c.StartMs)
		}
	}
}

func TestGetAdaptiveChunks_NewestFirstAndContiguous(t *testing.T) {
	startMs, endMs := int64(0), int64(90*60*1000)
	cfg := GetAdaptiveLoadingConfig(AdaptiveLoadingInput{StartMs: startMs, EndMs: endMs})
	chunks := GetAdaptiveChunks(startMs, endMs, cfg)
	if chunks[0].EndMs != endMs {
		t.Fatalf("first chunk should end at endMs, got %d", chunks[0].EndMs)
	}
	if chunks[len(chunks)-1].StartMs != startMs {
		t.Fatalf("last chunk should start at startMs, got %d", chunks[len(chunks)-1].StartMs)
	}
	for i := 1; i < len(chunks); i++ {
		if chunks[i-1].StartMs != chunks[i].EndMs {
			t.Fatalf("chunks not contiguous at index %d: %d != %d", i, chunks[i-1].StartMs, chunks[i].EndMs)
		}
	}
}

func TestGetAdaptiveChunks_EmptyRange(t *testing.T) {
	cfg := GetAdaptiveLoadingConfig(AdaptiveLoadingInput{StartMs: 100, EndMs: 100})
	if chunks := GetAdaptiveChunks(100, 100, cfg); chunks != nil {
		t.Fatalf("expected nil chunks for empty range, got %#v", chunks)
	}
}

func TestGetAdaptiveChunks_BodyParseOptimizationUsesHourChunks(t *testing.T) {
	startMs, endMs := int64(0), int64(4*OneHourMs)
	cfg := GetAdaptiveLoadingConfig(AdaptiveLoadingInput{
		StartMs: startMs,
		EndMs:   endMs,
		Pipeline: []map[string]any{
			{"type": "filter", "query": map[string]any{"$contains": []any{"Body", "x"}}},
		},
		ShouldOptimizeLineFilterQuery: true,
	})
	if cfg.ChunkSizeMs != OneHourMs {
		t.Fatalf("body-parse > 1h with optimization should pick 1-hour chunks, got %dms", cfg.ChunkSizeMs)
	}
	chunks := GetAdaptiveChunks(startMs, endMs, cfg)
	if len(chunks) != 4 {
		t.Fatalf("4h range with 1h chunks should produce 4 chunks, got %d", len(chunks))
	}
}
