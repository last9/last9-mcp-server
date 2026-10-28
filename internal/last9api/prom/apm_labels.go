package prom

import (
	"context"
	"net/http"

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
