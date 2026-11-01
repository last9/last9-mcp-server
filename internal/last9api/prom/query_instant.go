package prom

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"last9-mcp/internal/last9api"
)

const pathQueryInstant = "/prom_query_instant"

const (
	opQueryInstant    = "prometheus instant query"
	opQueryInstantRaw = "Prometheus instant query"
)

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
func QueryInstantRaw(ctx context.Context, c *last9api.Client, in QueryInstantInput) ([]byte, error) {
	var raw []byte
	err := c.Do(ctx, opQueryInstantRaw, last9api.Request{Method: http.MethodPost, Path: pathQueryInstant, Body: in}, &raw)
	return raw, err
}

// DiscoverServiceNames calls POST /prom_query_instant for a scoped service inventory.
func DiscoverServiceNames(ctx context.Context, c *last9api.Client, in QueryInstantInput) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var raw []byte
	if err := c.Do(ctx, "service name discovery", last9api.Request{Method: http.MethodPost, Path: pathQueryInstant, Body: in}, &raw, last9api.WithMaxResponseBytes(1<<20)); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("service name discovery: %w", err)
	}
	var series []InstantSeries
	if err := json.Unmarshal(raw, &series); err != nil {
		return nil, fmt.Errorf("service name discovery: invalid listing: %w", err)
	}
	if series == nil || len(series) > 10000 {
		return nil, fmt.Errorf("service name discovery: missing or oversized listing")
	}
	names := make([]string, 0, len(series))
	for _, item := range series {
		name := item.Metric["service_name"]
		if strings.TrimSpace(name) == "" || len(name) > 512 {
			return nil, fmt.Errorf("service name discovery: invalid service name")
		}
		names = append(names, name)
	}
	return names, nil
}
