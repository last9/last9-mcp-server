package entities

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"last9-mcp/internal/last9api"
)

const (
	pathEntityKPIs = "/entities/%s/kpis"
	// The create route requires the trailing slash; the API does not redirect.
	pathCreateEntityKPI = "/entities/%s/kpis/"
)

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

type CreateEntityKPIInput struct {
	EntityID     string
	Name         string
	Description  string
	DataSourceID string
	Query        string
	Unit         string
}

type createEntityKPIBody struct {
	Name         string        `json:"name"`
	Description  string        `json:"description,omitempty"`
	DataSourceID string        `json:"data_source_id,omitempty"`
	Definition   KPIDefinition `json:"definition"`
}

// CreateEntityKPI calls POST /entities/{entity_id}/kpis/.
func CreateEntityKPI(ctx context.Context, c *last9api.Client, in CreateEntityKPIInput) (EntityKPI, error) {
	body := createEntityKPIBody{
		Name:         in.Name,
		Description:  in.Description,
		DataSourceID: in.DataSourceID,
		Definition:   KPIDefinition{Query: in.Query, Unit: in.Unit},
	}
	var out EntityKPI
	path := fmt.Sprintf(pathCreateEntityKPI, url.PathEscape(in.EntityID))
	if err := c.Do(ctx, "create entity kpi", last9api.Request{Method: http.MethodPost, Path: path, Body: body}, &out); err != nil {
		return EntityKPI{}, err
	}
	return out, nil
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
