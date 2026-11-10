package logs

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"

	"last9-mcp/internal/last9api"
)

const pathQuerySeries = "/logs/api/v2/series/json"

type QuerySeriesInput struct {
	Start, End string
	Index      string
}

type QuerySeriesResponse struct {
	Data   []map[string]json.RawMessage `json:"data"`
	Status string                       `json:"status"`
	Result json.RawMessage              `json:"l9_result"`
}

// QuerySeries calls POST /logs/api/v2/series/json.
func QuerySeries(ctx context.Context, c *last9api.Client, in QuerySeriesInput) (QuerySeriesResponse, error) {
	query := url.Values{"start": {in.Start}, "end": {in.End}, "exact_bounds": {"true"}}
	if in.Index != "" {
		query.Set("index", in.Index)
	}
	var response QuerySeriesResponse
	err := c.Do(ctx, "log series", last9api.Request{Method: http.MethodPost, Path: pathQuerySeries, Body: map[string]any{"pipeline": []map[string]any{}}}, &response, last9api.WithQuery(query), last9api.WithRegionQuery())
	return response, err
}
