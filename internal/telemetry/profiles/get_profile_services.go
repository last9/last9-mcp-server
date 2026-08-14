package profiles

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// GetProfileServicesArgs lists services that have continuous profile samples.
type GetProfileServicesArgs struct {
	Env             string `json:"env,omitempty" jsonschema:"Filter by deployment.environment.name"`
	Cluster         string `json:"cluster,omitempty" jsonschema:"Filter by k8s.cluster.name"`
	Namespace       string `json:"namespace,omitempty" jsonschema:"Filter by k8s.namespace.name"`
	Runtime         string `json:"runtime,omitempty" jsonschema:"Filter by telemetry.sdk.language (go, java, python, …)"`
	ProfileType     string `json:"profile_type,omitempty" jsonschema:"Profile type: cpu (default), alloc, or wall"`
	Region          string `json:"region,omitempty" jsonschema:"Optional region override; defaults to the configured datasource region"`
	LookbackMinutes int    `json:"lookback_minutes,omitempty" jsonschema:"Minutes to look back from now (default: 60, minimum: 1)"`
	StartTimeISO    string `json:"start_time_iso,omitempty" jsonschema:"(Optional) Start time in RFC3339/ISO8601 format"`
	EndTimeISO      string `json:"end_time_iso,omitempty" jsonschema:"(Optional) End time in RFC3339/ISO8601 format"`
}

// NewGetProfileServicesHandler returns services with relative sample share in range.
func NewGetProfileServicesHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, GetProfileServicesArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args GetProfileServicesArgs) (*mcp.CallToolResult, any, error) {
		start, end, err := resolveProfileTimeRange(timeArgs{
			StartTimeISO:    args.StartTimeISO,
			EndTimeISO:      args.EndTimeISO,
			LookbackMinutes: args.LookbackMinutes,
		})
		if err != nil {
			return nil, nil, err
		}

		filters := filtersFromArgs("", args.Env, args.Cluster, args.Namespace, args.Runtime, args.ProfileType)
		region := args.Region

		sampleRows, err := runQueryRange(ctx, client, cfg, serviceIndexSamplePipeline(filters, DefaultFlamegraphRowLimit), start, end, DefaultFlamegraphRowLimit, region)
		if err != nil {
			return utils.ToolErrorResult(fmt.Sprintf("failed to fetch profile services: %v", err)), nil, nil
		}

		lastRows, err := runQueryRange(ctx, client, cfg, serviceIndexLastPipeline(filters, DefaultFlamegraphRowLimit), start, end, DefaultFlamegraphRowLimit, region)
		if err != nil {
			// Soft-fail last-profile probe — matches dashboard ProfilesApis.
			lastRows = nil
		}

		services := buildProfileServiceIndex(sampleRows, lastRows)
		result, err := jsonResult(map[string]any{
			"services":     services,
			"count":        len(services),
			"profile_type": string(filters.ProfileType),
			"start":        start.UTC().Format(time.RFC3339),
			"end":          end.UTC().Format(time.RFC3339),
		})
		return result, nil, err
	}
}
