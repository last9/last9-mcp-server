package profiles

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

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

func filtersFromArgs(service, env, cluster, namespace, runtime, profileType string) ProfileFilters {
	return ProfileFilters{
		Service:     strings.TrimSpace(service),
		Env:         strings.TrimSpace(env),
		Cluster:     strings.TrimSpace(cluster),
		Namespace:   strings.TrimSpace(namespace),
		Runtime:     strings.TrimSpace(runtime),
		ProfileType: normalizeProfileType(profileType),
	}
}
