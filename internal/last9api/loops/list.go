package loops

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"last9-mcp/internal/last9api"
)

const pathLoops = "/loops"

// ListLoops calls GET /loops.
func ListLoops(ctx context.Context, c *last9api.Client, query string) ([]Summary, error) {
	const op = "list loops"
	var raw json.RawMessage
	if err := c.Do(ctx, op, last9api.Request{Method: http.MethodGet, Path: pathLoops}, &raw, last9api.WithRequiredBody(), last9api.WithMaxResponseBytes(maxResponseBytes)); err != nil {
		return nil, mapError(err)
	}
	items, err := decodeSummaries(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	return filterSummaries(items, query), nil
}
