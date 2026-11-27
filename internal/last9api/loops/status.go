package loops

import (
	"context"
	"encoding/json"
	"net/http"

	"last9-mcp/internal/last9api"
)

// ProbeEnabled calls GET /loops.
func ProbeEnabled(ctx context.Context, c *last9api.Client) (bool, error) {
	var raw json.RawMessage
	err := c.Do(ctx, "get loops status", last9api.Request{Method: http.MethodGet, Path: pathLoops}, &raw, last9api.WithRequiredBody(), last9api.WithMaxResponseBytes(maxResponseBytes))
	if err == nil {
		return true, nil
	}
	if IsNotEnabled(mapError(err)) {
		return false, &NotEnabledError{}
	}
	return false, mapError(err)
}
