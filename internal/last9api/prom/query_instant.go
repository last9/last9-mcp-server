package prom

import (
	"context"
	"encoding/json"
	"errors"
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
	// A pointer, because Do accepts an empty body: a missing payload must fail
	// rather than read as zero series, which callers report as absence.
	var series *[]InstantSeries
	if err := c.Do(ctx, opQueryInstant, last9api.Request{Method: http.MethodPost, Path: pathQueryInstant, Body: in}, &series); err != nil {
		return nil, err
	}
	if series == nil {
		return nil, errors.New(opQueryInstant + ": empty response")
	}
	return *series, nil
}

// QueryInstantRaw calls POST /prom_query_instant.
func QueryInstantRaw(ctx context.Context, c *last9api.Client, in QueryInstantInput) (json.RawMessage, error) {
	var raw json.RawMessage
	err := c.Do(ctx, opQueryInstant, last9api.Request{Method: http.MethodPost, Path: pathQueryInstant, Body: in}, &raw)
	return raw, err
}
