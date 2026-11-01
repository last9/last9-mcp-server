// Package prom holds the Prometheus proxy endpoints.
package prom

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"last9-mcp/internal/last9api"
)

const pathLabelValues = "/prom_label_values"

const (
	opLabelValues    = "prometheus label values"
	opLabelValuesRaw = "Prometheus label values"
)

// Datasource is the Prometheus read endpoint the API proxies to.
type Datasource struct {
	ReadURL  string `json:"read_url"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type GetLabelValuesInput struct {
	Datasource
	Label   string   `json:"label"`
	Matches []string `json:"matches"`
	// End of the window, Unix seconds; Window is its length in seconds.
	Timestamp int64 `json:"timestamp"`
	Window    int64 `json:"window"`
	// Zero means no limit.
	Limit uint64 `json:"limit,omitempty"`
}

// GetLabelValues calls POST /prom_label_values.
func GetLabelValues(ctx context.Context, c *last9api.Client, in GetLabelValuesInput) ([]string, error) {
	var values []string
	err := c.Do(ctx, opLabelValues, last9api.Request{Method: http.MethodPost, Path: pathLabelValues, Body: in}, &values)
	return values, err
}

// GetLabelValuesRaw calls POST /prom_label_values.
func GetLabelValuesRaw(ctx context.Context, c *last9api.Client, in GetLabelValuesInput) ([]byte, error) {
	var raw []byte
	err := c.Do(ctx, opLabelValuesRaw, last9api.Request{Method: http.MethodPost, Path: pathLabelValues, Body: in}, &raw)
	return raw, err
}

// DiscoverLabelValues calls POST /prom_label_values without a result limit.
func DiscoverLabelValues(ctx context.Context, c *last9api.Client, in GetLabelValuesInput) ([]string, error) {
	if in.Limit != 0 {
		return nil, fmt.Errorf("name discovery requires an unlimited catalog")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var raw []byte
	if err := c.Do(ctx, "name discovery", last9api.Request{Method: http.MethodPost, Path: pathLabelValues, Body: in}, &raw, last9api.WithMaxResponseBytes(1<<20)); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("name discovery: %w", err)
	}
	return decodeDiscoveryNames(raw)
}
