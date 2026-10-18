package traces

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"

	"last9-mcp/internal/last9api"
)

const pathWaterfall = "/cat/api/traces/v2/waterfall/"

const opWaterfall = "trace waterfall"

// MaxWaterfallBytes is the largest evidence body the server-side sanitizer emits;
// anything bigger is refused rather than read.
const MaxWaterfallBytes = 512 << 10

type GetWaterfallInput struct {
	TraceID string
	// Unix seconds.
	Start, End int64
	MaxSpans   int
	// Optional; blank values are not sent.
	SelectedSpanID string
	Environment    string
}

// GetWaterfall calls GET /cat/api/traces/v2/waterfall/{traceId}. The body is
// returned undecoded: it is sanitized evidence meant to be relayed verbatim.
func GetWaterfall(ctx context.Context, c *last9api.Client, in GetWaterfallInput) (json.RawMessage, error) {
	q := url.Values{}
	q.Set("start", strconv.FormatInt(in.Start, 10))
	q.Set("end", strconv.FormatInt(in.End, 10))
	q.Set("max_spans", strconv.Itoa(in.MaxSpans))
	if in.SelectedSpanID != "" {
		q.Set("selected_span_id", in.SelectedSpanID)
	}
	if in.Environment != "" {
		q.Set("environment", in.Environment)
	}
	var body json.RawMessage
	req := last9api.Request{Method: http.MethodGet, Path: pathWaterfall + url.PathEscape(in.TraceID)}
	err := c.Do(ctx, opWaterfall, req, &body,
		last9api.WithQuery(q), last9api.WithRegionQuery(), last9api.WithMaxResponseBytes(MaxWaterfallBytes))
	return body, err
}
