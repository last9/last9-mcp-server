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

// RecipeQuery is one PromQL shape and the window rollup it applies ("none" plots it as returned).
// List queries keep a literal {duration} placeholder, in minutes.
type RecipeQuery struct {
	Rollup string `json:"rollup"`
	PromQL string `json:"promql"`
}

type RecipeQueries struct {
	Chart RecipeQuery `json:"chart"`
	List  RecipeQuery `json:"list"`
}

type MetricRecipe struct {
	Metric             string        `json:"metric"`
	Indicator          string        `json:"indicator"`
	Unit               string        `json:"unit"`
	Filters            []string      `json:"filters"`
	QuantileLabel      string        `json:"quantile_label,omitempty"`
	RecommendedQuery   string        `json:"recommended_query"`
	Queries            RecipeQueries `json:"queries"`
	ForbiddenFunctions []string      `json:"forbidden_functions"`
}

type ListMetricRecipesResponse struct {
	Recipes []MetricRecipe `json:"recipes"`
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
	// The overview catalog always lists its recipes; an empty or recipe-less body would
	// otherwise read as a catalog that covers nothing.
	if out == nil || len(out.Recipes) == 0 {
		return nil, errors.New(opMetricRecipes + ": response held no recipe catalog")
	}
	return out.Recipes, nil
}
