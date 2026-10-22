package prom

import (
	"context"
	"encoding/json"
	"net/http"

	"last9-mcp/internal/last9api"
)

const pathQuery = "/prom_query"

const opQuery = "prometheus range query"

type QueryInput struct {
	Datasource
	Query string `json:"query"`
	// End of the window, Unix seconds; Window is its length in seconds.
	Timestamp int64 `json:"timestamp"`
	Window    int64 `json:"window"`
}

// Query calls POST /prom_query.
func Query(ctx context.Context, c *last9api.Client, in QueryInput) (json.RawMessage, error) {
	var raw json.RawMessage
	err := c.Do(ctx, opQuery, last9api.Request{Method: http.MethodPost, Path: pathQuery, Body: in}, &raw)
	return raw, err
}
