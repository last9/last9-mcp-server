package servicerecipes

import (
	"context"
	"errors"
	"net/http"

	"last9-mcp/internal/last9api"
)

const pathMetricRecipes = "/recipes/metrics"

const opMetricRecipes = "metric recipes"

const maxMetricRecipesResponseBytes = 1 << 20

type RecipeRollups struct {
	Chart string `json:"chart"`
	List  string `json:"list"`
}

// List may hold a {duration} placeholder, in minutes.
type RecipeQueries struct {
	Chart string `json:"chart"`
	List  string `json:"list"`
}

type MetricRecipe struct {
	Metric             string        `json:"metric"`
	Indicator          string        `json:"indicator"`
	Unit               string        `json:"unit"`
	RecommendedQuery   string        `json:"recommended_query"`
	Rollups            RecipeRollups `json:"rollups"`
	Queries            RecipeQueries `json:"queries"`
	ForbiddenFunctions []string      `json:"forbidden_functions"`
}

type ListMetricRecipesResponse struct {
	Recipes *[]MetricRecipe `json:"recipes"`
}

// ListMetricRecipes calls GET /recipes/metrics.
func ListMetricRecipes(ctx context.Context, c *last9api.Client) ([]MetricRecipe, error) {
	var out *ListMetricRecipesResponse
	err := c.Do(ctx, opMetricRecipes,
		last9api.Request{Method: http.MethodGet, Path: pathMetricRecipes},
		&out,
		last9api.WithMaxResponseBytes(maxMetricRecipesResponseBytes),
	)
	if err != nil {
		return nil, err
	}
	// An empty or recipe-less body would otherwise read as a catalog that covers nothing.
	if out == nil || out.Recipes == nil {
		return nil, errors.New(opMetricRecipes + ": response held no recipe catalog")
	}
	return *out.Recipes, nil
}
