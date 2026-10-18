package infrastructure

import (
	"context"
	"encoding/json"
	"net/http"

	"last9-mcp/internal/last9api"
)

const pathResolve = "/infrastructure/resolve"

const opResolve = "infrastructure resolve"

const maxResolveResponseBytes = 256 * 1024

type ResolveInfrastructureInput struct {
	ClusterID  string            `json:"cluster_id"`
	EntityType string            `json:"entity_type"`
	Timestamp  int64             `json:"timestamp"`
	Selectors  map[string]string `json:"selectors"`
}

// ResolveInfrastructure calls POST /infrastructure/resolve. The response is returned
// verbatim because the tool relays it to the model unchanged.
func ResolveInfrastructure(
	ctx context.Context, c *last9api.Client, in ResolveInfrastructureInput,
) (json.RawMessage, error) {
	var out json.RawMessage
	err := c.Do(ctx, opResolve,
		last9api.Request{Method: http.MethodPost, Path: pathResolve, Body: in},
		&out,
		last9api.WithRegionHeader(),
		last9api.WithMaxResponseBytes(maxResolveResponseBytes),
	)
	if err != nil {
		return nil, err
	}
	return out, nil
}
