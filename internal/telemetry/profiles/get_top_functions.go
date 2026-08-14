package profiles

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// GetTopFunctionsArgs ranks hottest functions by self samples for a service.
type GetTopFunctionsArgs struct {
	Service         string `json:"service" jsonschema:"(Required) Service name (ServiceName) to profile"`
	Env             string `json:"env,omitempty" jsonschema:"Filter by deployment.environment.name"`
	Cluster         string `json:"cluster,omitempty" jsonschema:"Filter by k8s.cluster.name"`
	Namespace       string `json:"namespace,omitempty" jsonschema:"Filter by k8s.namespace.name"`
	Runtime         string `json:"runtime,omitempty" jsonschema:"Filter by telemetry.sdk.language"`
	ProfileType     string `json:"profile_type,omitempty" jsonschema:"Profile type: cpu (default), alloc, or wall"`
	Region          string `json:"region,omitempty" jsonschema:"Optional region override; defaults to the configured datasource region"`
	Limit           int    `json:"limit,omitempty" jsonschema:"Max ranked functions to return after folding (default: 50)"`
	LookbackMinutes int    `json:"lookback_minutes,omitempty" jsonschema:"Minutes to look back from now (default: 60, minimum: 1)"`
	StartTimeISO    string `json:"start_time_iso,omitempty" jsonschema:"(Optional) Start time in RFC3339/ISO8601 format"`
	EndTimeISO      string `json:"end_time_iso,omitempty" jsonschema:"(Optional) End time in RFC3339/ISO8601 format"`
}

// NewGetTopFunctionsHandler returns functions ranked by self sample share.
func NewGetTopFunctionsHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, GetTopFunctionsArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args GetTopFunctionsArgs) (*mcp.CallToolResult, any, error) {
		if strings.TrimSpace(args.Service) == "" {
			return nil, nil, fmt.Errorf("service is required")
		}

		start, end, err := resolveProfileTimeRange(timeArgs{
			StartTimeISO:    args.StartTimeISO,
			EndTimeISO:      args.EndTimeISO,
			LookbackMinutes: args.LookbackMinutes,
		})
		if err != nil {
			return nil, nil, err
		}

		rankLimit := args.Limit
		if rankLimit <= 0 {
			rankLimit = DefaultTopFunctionsLimit
		}

		filters := filtersFromArgs(args.Service, args.Env, args.Cluster, args.Namespace, args.Runtime, args.ProfileType)
		rows, err := runQueryRange(ctx, client, cfg, flamegraphPipeline(filters, DefaultFlamegraphRowLimit), start, end, DefaultFlamegraphRowLimit, args.Region)
		if err != nil {
			return utils.ToolErrorResult(fmt.Sprintf("failed to fetch top functions: %v", err)), nil, nil
		}

		functions := limitTopFunctions(FoldToTopFunctions(mapFlamegraphRows(rows)), rankLimit)
		result, err := jsonResult(map[string]any{
			"service":        filters.Service,
			"profile_type":   string(filters.ProfileType),
			"start":          start.UTC().Format(time.RFC3339),
			"end":            end.UTC().Format(time.RFC3339),
			"total_samples":  getProfileTotalSamples(functions),
			"function_count": len(functions),
			"functions":      functions,
		})
		return result, nil, err
	}
}
