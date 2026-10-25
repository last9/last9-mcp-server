package apm

import (
	"context"
	"runtime"
	"strings"
	"testing"

	"last9-mcp/internal/models"
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

func TestDatasourceKeepsNumbersApartAcrossSeparators(t *testing.T) {
	cfg := models.Config{Datasources: []models.DatasourceInfo{
		{Name: "db123", ReadURL: "https://db123", Username: "u", Password: "p", Region: "r"},
		{Name: "metrics-v12.3", ReadURL: "https://metrics", Username: "u", Password: "p", Region: "r"},
	}}
	for _, input := range []string{"db-1-2-3", "metrics-v1.23"} {
		if choice, err := chooseDatasource(cfg, input); err == nil {
			t.Errorf("chooseDatasource(%q) picked %q, want a not-found error", input, choice.cfg.PrometheusReadURL)
		}
	}
	if _, err := chooseDatasource(cfg, "db123"); err != nil {
		t.Errorf("exact control: %v", err)
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
