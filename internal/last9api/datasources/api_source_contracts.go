package datasources

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"last9-mcp/internal/last9api"
)

type GetAPISourceContractsInput struct {
	DatasourceID  string
	Index         string
	SchemaVersion int
}

// GetAPISourceContracts calls GET /datasources/{id}/api-source-contracts/.
func GetAPISourceContracts(ctx context.Context, c *last9api.Client, in GetAPISourceContractsInput) (json.RawMessage, error) {
	path := fmt.Sprintf("/datasources/%s/api-source-contracts/", url.PathEscape(in.DatasourceID))
	query := url.Values{"schema_version": {strconv.Itoa(in.SchemaVersion)}}
	if in.Index != "" {
		query.Set("index", in.Index)
	}
	var response json.RawMessage
	err := c.Do(ctx, "API source contracts", last9api.Request{Method: http.MethodGet, Path: path}, &response, last9api.WithQuery(query), last9api.WithRequiredBody())
	return response, err
}
