package utils

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestResultEnvelopeEmptyHasNoFields(t *testing.T) {
	var env ResultEnvelope
	if !env.IsEmpty() {
		t.Fatal("zero envelope reports non-empty")
	}
	encoded, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != "{}" {
		t.Fatalf("zero envelope encodes as %s, want {}", encoded)
	}
}

func TestResultEnvelopeEachMarkerMakesItNonEmpty(t *testing.T) {
	cases := map[string]func(*ResultEnvelope){
		"skip":         func(e *ResultEnvelope) { e.Skip("count sanity") },
		"truncate":     func(e *ResultEnvelope) { e.Truncate(10) },
		"hint":         func(e *ResultEnvelope) { e.Hint("widen the window") },
		"trim":         func(e *ResultEnvelope) { e.Trim(3, MaxResultTextBytes) },
		"continuation": func(e *ResultEnvelope) { e.Continuation = map[string]any{"cursor": "abc"} },
	}
	for name, mark := range cases {
		t.Run(name, func(t *testing.T) {
			var env ResultEnvelope
			mark(&env)
			if env.IsEmpty() {
				t.Fatalf("%s left the envelope empty", name)
			}
		})
	}
}

func TestResultEnvelopeSkipMarksPartial(t *testing.T) {
	var env ResultEnvelope
	env.Skip("chunk 2/3")
	env.Skip("chunk 3/3")
	if !env.Partial {
		t.Fatal("Skip did not set partial")
	}
	if strings.Join(env.Skipped, ",") != "chunk 2/3,chunk 3/3" {
		t.Fatalf("skipped = %v", env.Skipped)
	}
}

func TestResultEnvelopeTruncateRecordsLimit(t *testing.T) {
	var env ResultEnvelope
	env.Truncate(100)
	if !env.Truncated || env.Limit != 100 {
		t.Fatalf("truncated=%v limit=%d, want true/100", env.Truncated, env.Limit)
	}
}

func TestResultEnvelopeTrimRecordsCountAndHint(t *testing.T) {
	var env ResultEnvelope
	env.Trim(0, 500)
	if !env.IsEmpty() {
		t.Fatal("Trim(0) must not mark the envelope")
	}
	env.Trim(7, 500)
	if env.TrimmedItems != 7 {
		t.Fatalf("trimmed_items = %d, want 7", env.TrimmedItems)
	}
	if len(env.Hints) != 1 || !strings.Contains(env.Hints[0], "7 items") || !strings.Contains(env.Hints[0], "500 bytes") {
		t.Fatalf("hints = %v, want one hint naming the omitted count and the budget", env.Hints)
	}
}

func sizeOfJSON(items []string) int {
	encoded, _ := json.Marshal(items)
	return len(encoded)
}

func TestFitItemsKeepsEverythingUnderBudget(t *testing.T) {
	items := []string{"a", "b", "c"}
	kept, omitted := FitItems(items, sizeOfJSON(items), sizeOfJSON)
	if len(kept) != 3 || omitted != 0 {
		t.Fatalf("kept %d omitted %d, want 3/0", len(kept), omitted)
	}
}

func TestFitItemsKeepsLongestPrefixWithinBudget(t *testing.T) {
	items := []string{"aaaa", "bbbb", "cccc", "dddd"}
	budget := sizeOfJSON(items[:2])
	for _, tc := range []struct {
		budget   int
		wantKept int
	}{
		{budget - 1, 1},
		{budget, 2},
		{budget + 1, 2},
		{sizeOfJSON(items) - 1, 3},
		{1, 0},
	} {
		kept, omitted := FitItems(items, tc.budget, sizeOfJSON)
		if len(kept) != tc.wantKept || omitted != len(items)-tc.wantKept {
			t.Errorf("budget %d: kept %d omitted %d, want %d/%d", tc.budget, len(kept), omitted, tc.wantKept, len(items)-tc.wantKept)
		}
	}
}

func queryResponse(items int, itemBytes int) map[string]any {
	result := make([]any, items)
	for i := range result {
		result[i] = strings.Repeat("x", itemBytes)
	}
	return map[string]any{"status": "success", "data": map[string]any{"result": result}}
}

func TestFormatQueryResponseLeavesSmallResponsesWhole(t *testing.T) {
	var env ResultEnvelope
	response := queryResponse(3, 10)
	text := FormatQueryResponse(response, &env)

	want, _ := json.MarshalIndent(queryResponse(3, 10), "", "  ")
	if text != string(want) || !env.IsEmpty() {
		t.Fatalf("text changed or envelope marked: %q %+v", text, env)
	}
}

func TestFormatQueryResponseTrimsToBudget(t *testing.T) {
	var env ResultEnvelope
	response := queryResponse(50, 4000)
	text := FormatQueryResponse(response, &env)

	if len(text) > MaxResultTextBytes {
		t.Fatalf("text is %d bytes, over budget", len(text))
	}
	kept := len(response["data"].(map[string]any)["result"].([]any))
	if kept == 0 || kept+env.TrimmedItems != 50 {
		t.Fatalf("kept %d trimmed %d, want a non-empty prefix and the rest counted", kept, env.TrimmedItems)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(text), &decoded); err != nil {
		t.Fatalf("trimmed text is not JSON: %v", err)
	}
}

func TestFormatQueryResponseWithoutResultArrayIsUntouched(t *testing.T) {
	var env ResultEnvelope
	response := map[string]any{"blob": strings.Repeat("x", MaxResultTextBytes)}
	FormatQueryResponse(response, &env)
	if !env.IsEmpty() {
		t.Fatalf("envelope = %+v, want nothing trimmed when there is no data.result", env)
	}
}

func TestEnvelopeToolResultWithoutMarkers(t *testing.T) {
	result := EnvelopeToolResult(`{"rows":[1]}`, ResultEnvelope{})

	if len(result.Content) != 1 {
		t.Fatalf("content blocks = %d, want only the payload", len(result.Content))
	}
	if text := result.Content[0].(*mcp.TextContent).Text; text != `{"rows":[1]}` {
		t.Fatalf("payload text = %q", text)
	}
	if result.StructuredContent != nil {
		t.Fatalf("structuredContent = %#v, want the payload sent once", result.StructuredContent)
	}
}

func TestEnvelopeToolResultJoinsObjectPayload(t *testing.T) {
	var env ResultEnvelope
	env.Truncate(5)
	env.Hint("lower the limit")
	for name, text := range map[string]string{
		"compact":  `{"rows":[1]}`,
		"indented": "{\n  \"rows\": [\n    1\n  ]\n}\n",
		"empty":    "{}",
	} {
		t.Run(name, func(t *testing.T) {
			result := EnvelopeToolResult(text, env)
			if len(result.Content) != 1 || result.StructuredContent != nil {
				t.Fatalf("content blocks = %d structured = %#v, want one text block", len(result.Content), result.StructuredContent)
			}
			var payload map[string]any
			if err := json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &payload); err != nil {
				t.Fatalf("payload is not one JSON object: %v", err)
			}
			got, _ := payload[EnvelopeKey].(map[string]any)
			if got["truncated"] != true || got["limit"] != 5.0 {
				t.Fatalf("%s = %#v", EnvelopeKey, payload[EnvelopeKey])
			}
			if name != "empty" && len(payload) != 2 {
				t.Fatalf("payload members = %v, want rows and the envelope", payload)
			}
		})
	}
}

func TestEnvelopeToolResultAppendsBlockToNonObjectPayload(t *testing.T) {
	var env ResultEnvelope
	env.Hint("no series matched")
	encoded, _ := json.Marshal(env)
	for _, text := range []string{"[]", "Query not run: rejected."} {
		result := EnvelopeToolResult(text, env)
		if len(result.Content) != 2 || result.Content[0].(*mcp.TextContent).Text != text {
			t.Fatalf("%q: content = %#v, want the payload untouched plus an envelope block", text, result.Content)
		}
		if block := result.Content[1].(*mcp.TextContent).Text; block != "envelope: "+string(encoded) {
			t.Fatalf("envelope block = %q", block)
		}
	}
}
