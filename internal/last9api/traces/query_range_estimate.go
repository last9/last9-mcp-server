package traces

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	"last9-mcp/internal/last9api"
)

const pathQueryRangeEstimate = "/cat/api/traces/v2/query_range/json/estimate"

const opQueryRangeEstimate = "traces query estimate"

// EstimateQueryRangeInput mirrors the traces query_range call being estimated.
type EstimateQueryRangeInput struct {
	Pipeline any
	StartMs  int64
	EndMs    int64
}

// EstimateQueryRange calls POST /cat/api/traces/v2/query_range/json/estimate.
func EstimateQueryRange(ctx context.Context, c *last9api.Client, in EstimateQueryRangeInput) (last9api.QueryEstimate, error) {
	q := url.Values{}
	q.Set("direction", "backward")
	q.Set("start", strconv.FormatInt(in.StartMs/1000, 10))
	q.Set("end", strconv.FormatInt(in.EndMs/1000, 10))

	var out last9api.QueryEstimateResponse
	err := c.Do(ctx, opQueryRangeEstimate,
		last9api.Request{Method: http.MethodPost, Path: pathQueryRangeEstimate, Body: map[string]any{"pipeline": in.Pipeline}},
		&out,
		last9api.WithQuery(q), last9api.WithRegionQuery(),
	)
	if err != nil {
		return last9api.QueryEstimate{}, err
	}
	return out.Estimate()
}
