package loops

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"last9-mcp/internal/last9api"
)

const pathLoopByID = "/loops/%s"

// GetLoop calls GET /loops/{id}.
func GetLoop(ctx context.Context, c *last9api.Client, id string) (*Detail, error) {
	const op = "get loop"
	path := fmt.Sprintf(pathLoopByID, url.PathEscape(id))
	var raw json.RawMessage
	if err := c.Do(ctx, op, last9api.Request{Method: http.MethodGet, Path: path}, &raw, last9api.WithRequiredBody(), last9api.WithMaxResponseBytes(maxResponseBytes)); err != nil {
		return nil, mapError(err)
	}
	detail, err := decodeDetail(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	return detail, nil
}
