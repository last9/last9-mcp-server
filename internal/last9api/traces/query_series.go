package traces

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"

	"last9-mcp/internal/last9api"
)

const pathQuerySeries = "/cat/api/traces/v2/series/json"

type QuerySeriesInput struct {
	Start, End string
}

type QuerySeriesResponse struct {
	Data   []map[string]json.RawMessage `json:"data"`
	Status string                       `json:"status"`
	Result json.RawMessage              `json:"l9_result"`
}

// QuerySeries calls POST /cat/api/traces/v2/series/json.
func QuerySeries(ctx context.Context, c *last9api.Client, in QuerySeriesInput) (QuerySeriesResponse, error) {
	query := url.Values{"start": {in.Start}, "end": {in.End}, "exact_bounds": {"true"}}
	var response QuerySeriesResponse
	err := c.Do(ctx, "trace series", last9api.Request{Method: http.MethodPost, Path: pathQuerySeries, Body: map[string]any{"pipeline": []map[string]any{}}}, &response, last9api.WithQuery(query), last9api.WithRegionQuery())
	return response, err
}
