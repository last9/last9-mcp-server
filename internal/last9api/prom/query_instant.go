package prom

import (
	"context"
	"net/http"

	"last9-mcp/internal/last9api"
)

const pathQueryInstant = "/prom_query_instant"

const opQueryInstant = "prometheus instant query"

type QueryInstantInput struct {
	Datasource
	Query string `json:"query"`
	// Evaluation time, Unix seconds.
	Timestamp int64 `json:"timestamp"`
}

type InstantSeries struct {
	Metric map[string]string `json:"metric"`
	Value  []any             `json:"value"`
}

// QueryInstant calls POST /prom_query_instant.
func QueryInstant(ctx context.Context, c *last9api.Client, in QueryInstantInput) ([]InstantSeries, error) {
	var series []InstantSeries
	err := c.Do(ctx, opQueryInstant, last9api.Request{Method: http.MethodPost, Path: pathQueryInstant, Body: in}, &series)
	return series, err
}
