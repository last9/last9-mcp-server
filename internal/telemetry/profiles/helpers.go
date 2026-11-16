package profiles

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"last9-mcp/internal/deeplink"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type timeArgs struct {
	StartTimeISO    string
	EndTimeISO      string
	LookbackMinutes int
}

func resolveProfileTimeRange(args timeArgs) (time.Time, time.Time, error) {
	lookback := args.LookbackMinutes
	if lookback == 0 {
		lookback = DefaultLookbackMinutes
	}
	params := map[string]any{
		"lookback_minutes": lookback,
	}
	if args.StartTimeISO != "" {
		params["start_time_iso"] = args.StartTimeISO
	}
	if args.EndTimeISO != "" {
		params["end_time_iso"] = args.EndTimeISO
	}
	return utils.GetTimeRange(params, DefaultLookbackMinutes)
}

func jsonResult(v any) (*mcp.CallToolResult, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal result: %w", err)
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(b)}},
	}, nil
}

func filtersFromArgs(service, env, cluster, namespace, runtime, profileType string) (ProfileFilters, error) {
	pt, err := parseProfileType(profileType)
	if err != nil {
		return ProfileFilters{}, err
	}
	return ProfileFilters{
		Service:     strings.TrimSpace(service),
		Env:         strings.TrimSpace(env),
		Cluster:     strings.TrimSpace(cluster),
		Namespace:   strings.TrimSpace(namespace),
		Runtime:     strings.TrimSpace(runtime),
		ProfileType: pt,
	}, nil
}

// Profiles page tabs for profileTab.
const (
	profileTabFlamegraph   = "flamegraph"
	profileTabTopFunctions = "top-functions"
)

// withProfilesLink attaches a Profiles page deep link over the resolved window.
// The profile type is set only when the caller passed one. Error results carry
// no link.
func withProfilesLink(res *mcp.CallToolResult, cfg models.Config, start, end time.Time, filters ProfileFilters, rawProfileType, tab string) *mcp.CallToolResult {
	if res == nil || res.IsError {
		return res
	}
	params := deeplink.ProfilesLinkParams{
		Service:    filters.Service,
		Env:        filters.Env,
		Cluster:    filters.Cluster,
		Namespace:  filters.Namespace,
		Runtime:    filters.Runtime,
		ProfileTab: tab,
	}
	if strings.TrimSpace(rawProfileType) != "" {
		params.ProfileType = string(filters.ProfileType)
	}
	link := deeplink.NewBuilder(cfg.OrgSlug, cfg.ClusterID).BuildProfilesLink(start.UnixMilli(), end.UnixMilli(), params)
	res.Meta = deeplink.ToMeta(link)
	return res
}
