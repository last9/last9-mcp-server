package traces

import (
	"context"
	"net/http"

	"last9-mcp/internal/last9api"
)

const pathPromRange = "/prom_query"

const opPromRange = "trace metrics range query"

type QueryPromRangeInput struct {
	Query    string
	Start    int64
	End      int64
	Step     int64
	ReadURL  string
	Username string
	Password string
}

type PromRangeSeries struct {
	Metric map[string]string `json:"metric"`
	Values [][]any           `json:"values"`
}

type queryPromRangeRequest struct {
	Query     string `json:"query"`
	Timestamp int64  `json:"timestamp"`
	Window    int64  `json:"window"`
	Step      int64  `json:"step,omitempty"`
	ReadURL   string `json:"read_url"`
	Username  string `json:"username"`
	Password  string `json:"password"`
}

// QueryPromRange calls POST /prom_query.
func QueryPromRange(ctx context.Context, c *last9api.Client, in QueryPromRangeInput) ([]PromRangeSeries, error) {
	body := queryPromRangeRequest{
		Query:     in.Query,
		Timestamp: in.End,
		Window:    in.End - in.Start,
		Step:      in.Step,
		ReadURL:   in.ReadURL,
		Username:  in.Username,
		Password:  in.Password,
	}
	var out []PromRangeSeries
	err := c.Do(ctx, opPromRange,
		last9api.Request{Method: http.MethodPost, Path: pathPromRange, Body: body},
		&out,
		last9api.WithRequiredBody(),
	)
	return out, err
}
