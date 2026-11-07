package dashboards

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"last9-mcp/internal/last9api"
)

const pathDashboardByID = "/dashboards/%s"

// maxDashboardBytes matches the cap the dashboard tools apply to every success body.
const maxDashboardBytes = 5 << 20

type GetDashboardResponse struct {
	Dashboard map[string]any  `json:"dashboard"`
	Metadata  json.RawMessage `json:"metadata,omitempty"`
}

// GetDashboard calls GET /dashboards/{id}?region=.
func GetDashboard(ctx context.Context, c *last9api.Client, id string) (*GetDashboardResponse, error) {
	var out GetDashboardResponse
	req := last9api.Request{Method: http.MethodGet, Path: dashboardPath(id)}
	if err := c.Do(ctx, "get dashboard", req, &out,
		last9api.WithRegionQuery(), last9api.WithMaxResponseBytes(maxDashboardBytes)); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateDashboard calls PUT /dashboards/{id} with a {dashboard, metadata} body
// and returns the response body as sent.
func UpdateDashboard(ctx context.Context, c *last9api.Client, id string, body json.RawMessage) ([]byte, error) {
	var out []byte
	req := last9api.Request{Method: http.MethodPut, Path: dashboardPath(id), Body: body}
	if err := c.Do(ctx, "update dashboard", req, &out, last9api.WithMaxResponseBytes(maxDashboardBytes)); err != nil {
		return nil, err
	}
	return out, nil
}

// DeleteDashboard calls DELETE /dashboards/{id} and returns the response body as sent.
func DeleteDashboard(ctx context.Context, c *last9api.Client, id string) ([]byte, error) {
	var out []byte
	req := last9api.Request{Method: http.MethodDelete, Path: dashboardPath(id)}
	if err := c.Do(ctx, "delete dashboard", req, &out, last9api.WithMaxResponseBytes(maxDashboardBytes)); err != nil {
		return nil, err
	}
	return out, nil
}

func dashboardPath(id string) string {
	return fmt.Sprintf(pathDashboardByID, url.PathEscape(id))
}
