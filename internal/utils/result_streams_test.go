package utils

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestFormatQueryResponseTrimsStreamLines(t *testing.T) {
	for _, tc := range []struct {
		name   string
		counts []int
		bytes  int
	}{
		{"one stream", []int{40}, 4000},
		{"cross stream boundary", []int{2, 40, 3}, 4000},
		{"empty streams", []int{0, 40, 0}, 4000},
		{"line cannot fit", []int{2}, MaxResultTextBytes},
	} {
		t.Run(tc.name, func(t *testing.T) {
			items := make([]any, len(tc.counts))
			total := 0
			for i, count := range tc.counts {
				values := make([]any, count)
				for j := range values {
					values[j] = []any{fmt.Sprint(total + j), strings.Repeat("x", tc.bytes)}
				}
				total += count
				items[i] = map[string]any{"stream": map[string]any{"service": fmt.Sprint(i)}, "values": values}
			}
			original := formatIndentedJSON(items)
			response := map[string]any{"data": map[string]any{"resultType": "streams", "result": items}}
			var env ResultEnvelope
			text := FormatQueryResponse(response, &env)
			if len(text) > MaxResultTextBytes {
				t.Fatalf("payload exceeds budget: %d", len(text))
			}
			kept := response["data"].(map[string]any)["result"].([]any)
			count := 0
			for _, item := range kept {
				stream := item.(map[string]any)
				values := stream["values"].([]any)
				for _, value := range values {
					if value.([]any)[0] != fmt.Sprint(count) {
						t.Fatalf("not a prefix: line %v at %d", value, count)
					}
					count++
				}
				found := false
				for _, source := range items {
					if reflect.DeepEqual(stream["stream"], source.(map[string]any)["stream"]) {
						found = true
					}
				}
				if !found {
					t.Fatalf("lost labels: %v", stream["stream"])
				}
			}
			if tc.bytes < MaxResultTextBytes && count == 0 {
				t.Fatal("lost all lines that could fit")
			}
			if env.TrimmedItems != total-count {
				t.Fatalf("trimmed_items = %d, want %d", env.TrimmedItems, total-count)
			}
			if formatIndentedJSON(items) != original {
				t.Fatal("modified source streams while probing prefixes")
			}
		})
	}
}
