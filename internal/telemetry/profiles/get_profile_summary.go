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

// GetProfileSummaryArgs returns a short natural-language CPU/profile summary.
type GetProfileSummaryArgs struct {
	Service         string `json:"service" jsonschema:"(Required) Service name (ServiceName) to profile"`
	Env             string `json:"env,omitempty" jsonschema:"Filter by deployment.environment.name"`
	Cluster         string `json:"cluster,omitempty" jsonschema:"Filter by k8s.cluster.name"`
	Namespace       string `json:"namespace,omitempty" jsonschema:"Filter by k8s.namespace.name"`
	Runtime         string `json:"runtime,omitempty" jsonschema:"Filter by telemetry.sdk.language"`
	ProfileType     string `json:"profile_type,omitempty" jsonschema:"Profile type: cpu (default), alloc, or wall"`
	Region          string `json:"region,omitempty" jsonschema:"Optional region override; defaults to the configured datasource region"`
	TopN            int    `json:"top_n,omitempty" jsonschema:"How many top consumers to mention (default: 3, max: 10)"`
	LookbackMinutes int    `json:"lookback_minutes,omitempty" jsonschema:"Minutes to look back from now (default: 60, minimum: 1)"`
	StartTimeISO    string `json:"start_time_iso,omitempty" jsonschema:"(Optional) Start time in RFC3339/ISO8601 format"`
	EndTimeISO      string `json:"end_time_iso,omitempty" jsonschema:"(Optional) End time in RFC3339/ISO8601 format"`
}

// NewGetProfileSummaryHandler summarizes the hottest functions for a service.
func NewGetProfileSummaryHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, GetProfileSummaryArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args GetProfileSummaryArgs) (*mcp.CallToolResult, any, error) {
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

		topN := args.TopN
		if topN <= 0 {
			topN = 3
		}
		if topN > 10 {
			topN = 10
		}

		filters, err := filtersFromArgs(args.Service, args.Env, args.Cluster, args.Namespace, args.Runtime, args.ProfileType)
		if err != nil {
			return nil, nil, err
		}
		rows, err := runQueryRange(ctx, client, cfg, flamegraphPipeline(filters, DefaultFlamegraphRowLimit), start, end, DefaultFlamegraphRowLimit, args.Region)
		if err != nil {
			return utils.ToolErrorResult(fmt.Sprintf("failed to fetch profile summary: %v", err)), nil, nil
		}

		functions := FoldToTopFunctions(mapFlamegraphRows(rows))
		total := getProfileTotalSamples(functions)
		top := limitTopFunctions(functions, topN)
		summary := buildProfileSummaryText(filters.Service, string(filters.ProfileType), top, total)

		result, err := jsonResult(map[string]any{
			"service":       filters.Service,
			"profile_type":  string(filters.ProfileType),
			"start":         start.UTC().Format(time.RFC3339),
			"end":           end.UTC().Format(time.RFC3339),
			"total_samples": total,
			"summary":       summary,
			"top_functions": top,
		})
		return result, nil, err
	}
}

func buildProfileSummaryText(service, profileType string, top []TopFunction, total float64) string {
	if len(top) == 0 || total <= 0 {
		return fmt.Sprintf("No %s profile samples found for service %q in the selected window.", profileType, service)
	}

	names := make([]string, 0, len(top))
	var topShare float64
	for _, fn := range top {
		names = append(names, fn.Name)
		topShare += fn.SelfSamples
	}
	pct := (topShare / total) * 100

	label := "CPU"
	switch profileType {
	case string(ProfileTypeAlloc):
		label = "allocation"
	case string(ProfileTypeWall):
		label = "wall-time"
	}

	return fmt.Sprintf(
		"Top %d %s consumers for %s are %s, accounting for %.1f%% of total self samples.",
		len(top),
		label,
		service,
		strings.Join(names, ", "),
		pct,
	)
}
