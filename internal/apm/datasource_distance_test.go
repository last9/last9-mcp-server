package apm

import (
	"context"
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
