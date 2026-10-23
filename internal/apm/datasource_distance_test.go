package apm

import (
	"context"
	"runtime"
	"strings"
	"testing"
)

func TestDatasourceRejectsBeyondBudget(t *testing.T) {
	for _, input := range []string{"staging", "stapinx"} {
		t.Run(input, func(t *testing.T) {
			tools, readURLs := promToolsUnderTest(t)
			for tool, call := range tools {
				t.Run(tool, func(t *testing.T) {
					*readURLs = nil
					result, err := call(context.Background(), input)
					if input == "staging" {
						if err != nil || result == nil || result.IsError || len(*readURLs) != 1 {
							t.Fatalf("exact positive control: result=%v err=%v calls=%v", result, err, *readURLs)
						}
					} else if err == nil || len(*readURLs) != 0 {
						t.Fatalf("two edits exceed floor(7/4)=1: want error and zero calls; got err=%v calls=%v result=%v", err, *readURLs, result)
					}
				})
			}
		})
	}
}

func TestDatasourceLongUnknownAllocation(t *testing.T) {
	input := strings.Repeat("x", 256<<10)
	tools, readURLs := promToolsUnderTest(t)
	for tool, call := range tools {
		t.Run(tool, func(t *testing.T) {
			*readURLs = nil
			runtime.GC()
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			_, err := call(context.Background(), input)
			runtime.ReadMemStats(&after)
			if err == nil || len(*readURLs) != 0 {
				t.Fatalf("want rejection without upstream calls; got err=%v calls=%d", err, len(*readURLs))
			}
			if used := after.TotalAlloc - before.TotalAlloc; used > 8<<20 {
				t.Errorf("rejecting a %d-byte datasource allocated %d bytes; budget 8 MiB", len(input), used)
			}
		})
	}
}
