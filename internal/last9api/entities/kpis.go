package entities

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"last9-mcp/internal/last9api"
)

const pathEntityKPIs = "/entities/%s/kpis"

type KPIDefinition struct {
	Query  string `json:"query"`
	Unit   string `json:"unit"`
	Source string `json:"source,omitempty"`
}

type EntityKPI struct {
	ID           string        `json:"id"`
	Name         string        `json:"name"`
	Description  string        `json:"description,omitempty"`
	Definition   KPIDefinition `json:"definition"`
	KPIType      string        `json:"kpi_type,omitempty"`
	DataSourceID string        `json:"data_source_id,omitempty"`
}

// ListEntityKPIs calls GET /entities/{entity_id}/kpis.
func ListEntityKPIs(ctx context.Context, c *last9api.Client, entityID string) ([]EntityKPI, error) {
	var out []EntityKPI
	path := fmt.Sprintf(pathEntityKPIs, url.PathEscape(entityID))
	if err := c.Do(ctx, "list entity kpis", last9api.Request{Method: http.MethodGet, Path: path}, &out); err != nil {
		return nil, err
	}
	return out, nil
}
