// Package traces holds the trace query endpoints.
package traces

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	"last9-mcp/internal/last9api"
)

const pathSearchTags = "/cat/api/search/tags"

const opSearchTags = "trace tag catalog"

type ListSearchTagsInput struct {
	// Unix seconds.
	Start, End int64
}

// TagScope names span, resource or event tags, with the scope prefix stripped.
type TagScope struct {
	Name string   `json:"name"`
	Tags []string `json:"tags"`
}

type ListSearchTagsResponse struct {
	Scopes []TagScope `json:"scopes"`
}

// ListSearchTags calls GET /cat/api/search/tags.
func ListSearchTags(ctx context.Context, c *last9api.Client, in ListSearchTagsInput) (ListSearchTagsResponse, error) {
	q := url.Values{}
	q.Set("start", strconv.FormatInt(in.Start, 10))
	q.Set("end", strconv.FormatInt(in.End, 10))
	var resp ListSearchTagsResponse
	req := last9api.Request{Method: http.MethodGet, Path: pathSearchTags}
	err := c.Do(ctx, opSearchTags, req, &resp, last9api.WithQuery(q), last9api.WithRegionQuery())
	return resp, err
}
