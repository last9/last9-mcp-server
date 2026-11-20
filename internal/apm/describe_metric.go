package apm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"last9-mcp/internal/last9api"
	"last9-mcp/internal/last9api/servicerecipes"
	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	recipeSourceServiceRecipe   = "service_recipe"
	recipeSourceLegacyHeuristic = "legacy_heuristic"
	recipeSourceUncovered       = "uncovered"
	recipeMetricPrefix          = "trace_"
)

var bareMetricName = regexp.MustCompile(`^[a-zA-Z_:][a-zA-Z0-9_:]*$`)

const legacyBasis = "Legacy manual example, not the Discover Services dashboard query; the recipe catalog could not be read."

// legacyMetricExamples is the manual's guidance from before recipes, served only when the
// catalog read fails; it is not the dashboard query.
var legacyMetricExamples = map[string]DescribeMetricResult{
	"trace_endpoint_count": {
		Unit:             "req/s",
		ExampleQuery:     `sum(sum_over_time(trace_endpoint_count{span_kind="SPAN_KIND_SERVER"}[5m])) / 300`,
		CounterRuleQuery: "sum(rate(trace_endpoint_count[5m]))",
	},
	"trace_service_response_time": {
		Unit:         "seconds",
		ExampleQuery: "histogram_quantile(0.95, sum by (le) (rate(trace_service_response_time_bucket[5m])))",
	},
}

type DescribeMetricArgs struct {
	Metric string `json:"metric" jsonschema:"(Required) Bare metric name (e.g. trace_endpoint_count); a PromQL selector or expression is rejected"`
}

type DescribeMetricResult struct {
	Metric             string                        `json:"metric"`
	Source             string                        `json:"source"`
	Indicator          string                        `json:"indicator,omitempty"`
	Unit               string                        `json:"unit,omitempty"`
	Filters            []string                      `json:"filters,omitempty"`
	QuantileLabel      string                        `json:"quantile_label,omitempty"`
	RecommendedQuery   string                        `json:"recommended_query,omitempty"`
	Queries            *servicerecipes.RecipeQueries `json:"queries,omitempty"`
	ForbiddenFunctions []string                      `json:"forbidden_functions,omitempty"`
	ExampleQuery       string                        `json:"example_query,omitempty"`
	CounterRuleQuery   string                        `json:"counter_rule_query,omitempty"`
	Basis              string                        `json:"basis,omitempty"`
	Note               string                        `json:"note,omitempty"`
}

func NewDescribeMetricHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, DescribeMetricArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args DescribeMetricArgs) (*mcp.CallToolResult, any, error) {
		metric := strings.TrimSpace(args.Metric)
		if !bareMetricName.MatchString(metric) {
			return nil, nil, fmt.Errorf("metric must be a bare metric name such as trace_endpoint_count, not a PromQL selector or expression; got %q", args.Metric)
		}
		result, err := resolveMetricRecipe(ctx, last9api.NewClient(client, cfg), metric)
		if err != nil {
			return nil, nil, err
		}
		var payload bytes.Buffer
		enc := json.NewEncoder(&payload)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(result); err != nil {
			return nil, nil, fmt.Errorf("failed to marshal describe_metric response: %w", err)
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: strings.TrimSuffix(payload.String(), "\n")}},
		}, nil, nil
	}
}

func resolveMetricRecipe(ctx context.Context, c *last9api.Client, metric string) (DescribeMetricResult, error) {
	if !strings.HasPrefix(metric, recipeMetricPrefix) {
		return DescribeMetricResult{Metric: metric, Source: recipeSourceUncovered,
			Note: "Discover Services recipes cover trace_* metrics only."}, nil
	}
	recipes, err := servicerecipes.ListMetricRecipes(ctx, c)
	if err != nil {
		return readFailedResult(metric, err), nil
	}
	recipe, err := findMetricRecipe(recipes, metric)
	if err != nil {
		return DescribeMetricResult{}, err
	}
	if recipe == nil {
		return DescribeMetricResult{Metric: metric, Source: recipeSourceUncovered,
			Note: "The Discover Services recipe catalog has no entry for this metric."}, nil
	}
	return DescribeMetricResult{
		Metric:             metric,
		Source:             recipeSourceServiceRecipe,
		Indicator:          recipe.Indicator,
		Unit:               recipe.Unit,
		Filters:            recipe.Filters,
		QuantileLabel:      recipe.QuantileLabel,
		RecommendedQuery:   recipe.RecommendedQuery,
		Queries:            &recipe.Queries,
		ForbiddenFunctions: recipe.ForbiddenFunctions,
	}, nil
}

func findMetricRecipe(recipes []servicerecipes.MetricRecipe, metric string) (*servicerecipes.MetricRecipe, error) {
	var matches []servicerecipes.MetricRecipe
	for _, r := range recipes {
		if r.Metric == metric {
			matches = append(matches, r)
		}
	}
	switch {
	case len(matches) == 0:
		return nil, nil
	case len(matches) > 1:
		return nil, fmt.Errorf("recipe catalog lists %d recipes for %s; expected one", len(matches), metric)
	case strings.TrimSpace(matches[0].RecommendedQuery) == "":
		return nil, fmt.Errorf("recipe for %s has no recommended_query", metric)
	}
	return &matches[0], nil
}

func readFailedResult(metric string, err error) DescribeMetricResult {
	note := "Reading the Discover Services recipe catalog failed: " + err.Error()
	// The client's size error advises narrowing the request, which this tool has no argument for.
	var tooLarge *last9api.ResponseTooLargeError
	if errors.As(err, &tooLarge) {
		note = fmt.Sprintf("Reading the Discover Services recipe catalog failed: the catalog exceeds %d bytes.", tooLarge.MaxBytes)
	}
	legacy, ok := legacyMetricExamples[metric]
	if !ok {
		return DescribeMetricResult{Metric: metric, Source: recipeSourceUncovered, Note: note}
	}
	legacy.Metric = metric
	legacy.Source = recipeSourceLegacyHeuristic
	legacy.Basis = legacyBasis
	legacy.Note = note
	return legacy
}
