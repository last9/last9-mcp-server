package last9api

import "errors"

// QueryEstimate is what the logs and traces estimate endpoints report about a
// query before it runs: how much it would scan and whether the server sees a
// service or stream-level filter in it.
type QueryEstimate struct {
	Bytes  int64
	Rows   int64
	Scoped bool
}

// QueryEstimateResponse is the shared body of the logs and traces estimate endpoints.
type QueryEstimateResponse struct {
	Data *struct {
		Bytes  *int64 `json:"bytes"`
		Rows   int64  `json:"rows"`
		Scoped bool   `json:"scoped"`
	} `json:"data"`
}

// Estimate rejects a body without data.bytes: a missing size must read as
// unknown, never as a zero-byte scan that passes every budget.
func (r QueryEstimateResponse) Estimate() (QueryEstimate, error) {
	if r.Data == nil || r.Data.Bytes == nil {
		return QueryEstimate{}, errors.New("estimate response has no data.bytes")
	}
	return QueryEstimate{Bytes: *r.Data.Bytes, Rows: r.Data.Rows, Scoped: r.Data.Scoped}, nil
}
