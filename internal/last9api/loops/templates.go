package loops

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"last9-mcp/internal/last9api"
)

const pathLoopTemplates = "/loops/templates"

// ListTemplates calls GET /loops/templates.
func ListTemplates(ctx context.Context, c *last9api.Client, capability string) ([]Template, error) {
	const op = "list loop templates"
	var raw json.RawMessage
	if err := c.Do(ctx, op, last9api.Request{Method: http.MethodGet, Path: pathLoopTemplates}, &raw, last9api.WithRequiredBody(), last9api.WithMaxResponseBytes(maxResponseBytes)); err != nil {
		return nil, mapError(err)
	}
	items, err := decodeTemplates(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	return filterTemplates(items, capability), nil
}
