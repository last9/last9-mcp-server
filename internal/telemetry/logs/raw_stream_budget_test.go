package logs

import (
	"fmt"
	"strings"
	"testing"

	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestRawLogsPreserveLinesWhenOneStreamExceedsBudget(t *testing.T) {
	for _, tc := range []struct {
		count, bytes int
		mode         string
	}{
		{2, 4000, "raw"}, {40, 4000, "raw"}, {20, 6000, ""},
	} {
		t.Run(fmt.Sprintf("lines_%d", tc.count), func(t *testing.T) {
			server := streamsServer(t, nLogValues(tc.count, strings.Repeat("x", tc.bytes)), nil)
			result := callGetLogsEnvelope(t, server, GetLogsArgs{
				LogjsonQuery: rawErrorFilter, LookbackMinutes: 5, Limit: 50, ResponseMode: tc.mode,
			})
			payload := parseToolJSONResult(t, result)
			got := countEntriesInPayload(t, payload)
			if got == 0 {
				t.Fatal("all matching lines discarded although individual lines fit the response budget")
			}
			if tc.count == 2 && got != tc.count {
				t.Fatalf("small response lost lines: %d", got)
			}
			text := result.Content[0].(*mcp.TextContent).Text
			if len(text) > utils.MaxResultTextBytes {
				t.Fatalf("payload exceeds budget: %d bytes", len(text))
			}
			env := utils.GetResultEnvelope(t, result)
			if env.TrimmedItems != tc.count-got {
				t.Fatalf("trimmed_items = %d, want %d omitted lines", env.TrimmedItems, tc.count-got)
			}
			_, items, err := extractResultItems(payload)
			if err != nil {
				t.Fatal(err)
			}
			stream := items[0].(map[string]any)["stream"].(map[string]any)
			if stream["severity"] != "error" {
				t.Fatalf("lost stream labels: %v", stream)
			}
		})
	}
}
