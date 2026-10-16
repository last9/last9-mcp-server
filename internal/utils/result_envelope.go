package utils

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// MaxResultTextBytes bounds a query tool's payload text. MCP clients commonly cut
// tool output near 25k tokens without telling the model; trimming here first keeps
// the cut visible in the envelope.
const MaxResultTextBytes = 100_000

// ResultEnvelope reports how complete a query or read tool's result is. The zero
// value means the payload is the whole answer.
type ResultEnvelope struct {
	Partial      bool           `json:"partial,omitempty"`
	Skipped      []string       `json:"skipped,omitempty"`
	Truncated    bool           `json:"truncated,omitempty"`
	Limit        int            `json:"limit,omitempty"`
	Continuation map[string]any `json:"continuation,omitempty"`
	TrimmedItems int            `json:"trimmed_items,omitempty"`
	Hints        []string       `json:"hints,omitempty"`
}

// Skip records a stage or chunk that did not complete.
func (e *ResultEnvelope) Skip(part string) {
	e.Partial = true
	e.Skipped = append(e.Skipped, part)
}

// Truncate records that more rows matched than limit, the number returned.
func (e *ResultEnvelope) Truncate(limit int) {
	e.Truncated = true
	e.Limit = limit
}

func (e *ResultEnvelope) Hint(hint string) {
	e.Hints = append(e.Hints, hint)
}

// Trim records items dropped to keep the payload within MaxResultTextBytes.
func (e *ResultEnvelope) Trim(omitted int) {
	if omitted <= 0 {
		return
	}
	e.TrimmedItems = omitted
	e.Hint(fmt.Sprintf("Response trimmed to fit %d bytes: %d items omitted. Narrow the filters or time range, or aggregate, rather than raising limit.", MaxResultTextBytes, omitted))
}

func (e ResultEnvelope) IsEmpty() bool {
	return !e.Partial && len(e.Skipped) == 0 && !e.Truncated && e.Limit == 0 &&
		len(e.Continuation) == 0 && e.TrimmedItems == 0 && len(e.Hints) == 0
}

// FitItems returns the longest prefix of items whose rendered size is within
// maxBytes, and how many items it dropped. size must grow with the prefix.
func FitItems[T any](items []T, maxBytes int, size func([]T) int) ([]T, int) {
	if size(items) <= maxBytes {
		return items, 0
	}
	kept := sort.Search(len(items), func(n int) bool {
		return size(items[:n+1]) > maxBytes
	})
	return items[:kept], len(items) - kept
}

// EnvelopeToolResult returns text as the readable payload, adds the envelope as
// its own text block when it carries anything, and mirrors both as
// structuredContent {"result": payload, "envelope": env}.
func EnvelopeToolResult(payload any, text string, env ResultEnvelope) *mcp.CallToolResult {
	content := []mcp.Content{&mcp.TextContent{Text: text}}
	if !env.IsEmpty() {
		encoded, err := json.Marshal(env)
		if err == nil {
			content = append(content, &mcp.TextContent{Text: "envelope: " + string(encoded)})
		}
	}
	return &mcp.CallToolResult{
		Content: content,
		StructuredContent: map[string]any{
			"result":   payload,
			"envelope": env,
		},
	}
}
