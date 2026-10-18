// Package logs holds the log query endpoints.
package logs

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"last9-mcp/internal/last9api"
)

const pathLabels = "/logs/api/v1/labels"

const opLabels = "log labels"

type ListLabelsInput struct {
	// Unix seconds.
	Start, End int64
}

type listLabelsResponse struct {
	Status string   `json:"status"`
	Data   []string `json:"data"`
}

// ListLabels calls GET /logs/api/v1/labels.
func ListLabels(ctx context.Context, c *last9api.Client, in ListLabelsInput) ([]string, error) {
	q := url.Values{}
	q.Set("start", strconv.FormatInt(in.Start, 10))
	q.Set("end", strconv.FormatInt(in.End, 10))
	var resp listLabelsResponse
	req := last9api.Request{Method: http.MethodGet, Path: pathLabels}
	if err := c.Do(ctx, opLabels, req, &resp, last9api.WithQuery(q), last9api.WithRegionQuery()); err != nil {
		return nil, err
	}
	if resp.Status != "success" {
		return nil, fmt.Errorf("%s: API returned status %q", opLabels, resp.Status)
	}
	return resp.Data, nil
}
