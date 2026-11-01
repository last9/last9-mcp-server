package prom

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"last9-mcp/internal/last9api"
)

const pathAPMLabels = "/apm/labels"

const opAPMLabels = "Prometheus labels"

type GetAPMLabelsInput struct {
	Datasource
	Metric    string `json:"metric"`
	Timestamp int64  `json:"timestamp"`
	Window    int64  `json:"window"`
}

// GetAPMLabels calls POST /apm/labels.
func GetAPMLabels(ctx context.Context, c *last9api.Client, in GetAPMLabelsInput) ([]byte, error) {
	var raw []byte
	err := c.Do(ctx, opAPMLabels, last9api.Request{Method: http.MethodPost, Path: pathAPMLabels, Body: in}, &raw)
	return raw, err
}

// DiscoverLabelNames calls POST /apm/labels for a scoped label-name listing.
func DiscoverLabelNames(ctx context.Context, c *last9api.Client, in GetAPMLabelsInput) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var raw []byte
	if err := c.Do(ctx, "label name discovery", last9api.Request{Method: http.MethodPost, Path: pathAPMLabels, Body: in}, &raw, last9api.WithMaxResponseBytes(1<<20)); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("label name discovery: %w", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, fmt.Errorf("label name discovery: invalid listing: %w", err)
	}
	if err := checkDiscoveryMetadata(fields); err != nil {
		return nil, err
	}
	return decodeDiscoveryNames(fields["labels"])
}
