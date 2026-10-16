package entities

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"last9-mcp/internal/last9api"
)

const (
	pathEntityKPIs       = "/entities/%s/kpis"
	pathEntityKPIsCreate = "/entities/%s/kpis/"
)

type KPIDefinition struct {
	Query string `json:"query"`
	Unit  string `json:"unit"`
}

type KPI struct {
	ID         string        `json:"id"`
	Name       string        `json:"name"`
	Definition KPIDefinition `json:"definition"`
}

type CreateKPIInput struct {
	EntityID string
	Name     string
	Query    string
	Unit     string
}

type createKPIBody struct {
	Name       string        `json:"name"`
	Definition KPIDefinition `json:"definition"`
}

// ListEntityKPIs calls GET /entities/{entity_id}/kpis.
func ListEntityKPIs(ctx context.Context, c *last9api.Client, entityID string) ([]KPI, error) {
	path := fmt.Sprintf(pathEntityKPIs, url.PathEscape(entityID))
	var out []KPI
	err := c.Do(ctx, "list entity kpis", last9api.Request{Method: http.MethodGet, Path: path}, &out)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// CreateEntityKPI calls POST /entities/{entity_id}/kpis/ (trailing slash required).
func CreateEntityKPI(ctx context.Context, c *last9api.Client, in CreateKPIInput) (KPI, error) {
	path := fmt.Sprintf(pathEntityKPIsCreate, url.PathEscape(in.EntityID))
	var out KPI
	err := c.Do(ctx, "create entity kpi", last9api.Request{
		Method: http.MethodPost,
		Path:   path,
		Body: createKPIBody{
			Name:       in.Name,
			Definition: KPIDefinition{Query: in.Query, Unit: in.Unit},
		},
	}, &out)
	if err != nil {
		return KPI{}, err
	}
	return out, nil
}
