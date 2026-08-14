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

// GetFlamegraphArgs fetches a nested flamegraph for one service.
type GetFlamegraphArgs struct {
	Service         string `json:"service" jsonschema:"(Required) Service name (ServiceName) to profile"`
	Env             string `json:"env,omitempty" jsonschema:"Filter by deployment.environment.name"`
	Cluster         string `json:"cluster,omitempty" jsonschema:"Filter by k8s.cluster.name"`
	Namespace       string `json:"namespace,omitempty" jsonschema:"Filter by k8s.namespace.name"`
	Runtime         string `json:"runtime,omitempty" jsonschema:"Filter by telemetry.sdk.language"`
	ProfileType     string `json:"profile_type,omitempty" jsonschema:"Profile type: cpu (default), alloc, or wall. Prefer cpu unless the user asks for alloc/wall."`
	Region          string `json:"region,omitempty" jsonschema:"Optional region override; defaults to the configured datasource region"`
	Limit           int    `json:"limit,omitempty" jsonschema:"Max aggregated stack rows from the API (default: 1000)"`
	LookbackMinutes int    `json:"lookback_minutes,omitempty" jsonschema:"Minutes to look back from now (default: 60, minimum: 1)"`
	StartTimeISO    string `json:"start_time_iso,omitempty" jsonschema:"(Optional) Start time in RFC3339/ISO8601 format"`
	EndTimeISO      string `json:"end_time_iso,omitempty" jsonschema:"(Optional) End time in RFC3339/ISO8601 format"`
}

// NewGetFlamegraphHandler returns a structured flamegraph tree for a service.
func NewGetFlamegraphHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, GetFlamegraphArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args GetFlamegraphArgs) (*mcp.CallToolResult, any, error) {
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

		limit := args.Limit
		if limit <= 0 {
			limit = DefaultFlamegraphRowLimit
		}

		filters := filtersFromArgs(args.Service, args.Env, args.Cluster, args.Namespace, args.Runtime, args.ProfileType)
		rows, err := runQueryRange(ctx, client, cfg, flamegraphPipeline(filters, limit), start, end, limit, args.Region)
		if err != nil {
			return utils.ToolErrorResult(fmt.Sprintf("failed to fetch flamegraph: %v", err)), nil, nil
		}

		flameRows := mapFlamegraphRows(rows)
		tree := BuildFlameTree(flameRows)
		result, err := jsonResult(map[string]any{
			"service":      filters.Service,
			"profile_type": string(filters.ProfileType),
			"start":        start.UTC().Format(time.RFC3339),
			"end":          end.UTC().Format(time.RFC3339),
			"row_count":    len(flameRows),
			"truncated":    len(flameRows) >= limit,
			"total_samples": tree.Value,
			"flamegraph":   tree,
		})
		return result, nil, err
	}
}
