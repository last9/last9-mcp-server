package dashboards

import (
	"context"
	"net/http"

	"last9-mcp/internal/last9api"
)

type SetDashboardReadonlyResponse struct {
	ID       string `json:"id"`
	Readonly *bool  `json:"readonly"`
}

// SetDashboardReadonly calls PATCH /dashboards/{id}/readonly with a {readonly} body.
func SetDashboardReadonly(ctx context.Context, c *last9api.Client, id string, readonly bool) (*SetDashboardReadonlyResponse, error) {
	var out SetDashboardReadonlyResponse
	req := last9api.Request{
		Method: http.MethodPatch,
		Path:   dashboardPath(id) + "/readonly",
		Body:   map[string]bool{"readonly": readonly},
	}
	if err := c.Do(ctx, "set dashboard lock", req, &out, last9api.WithRequiredBody()); err != nil {
		return nil, err
	}
	return &out, nil
}
