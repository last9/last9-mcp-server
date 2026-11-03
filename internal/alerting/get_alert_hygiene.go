package alerting

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"last9-mcp/internal/deeplink"
	"last9-mcp/internal/last9api"
	"last9-mcp/internal/last9api/alerts"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	defaultHygieneLimit = 20
	maxHygieneLimit     = 100

	// limit bounds the count, not the bytes: names and expressions are user text.
	maxHygieneResponseBytes = 32 * 1024
)

type GetAlertHygieneArgs struct {
	Window string `json:"window,omitempty" jsonschema:"Lookback window: 7d or 30d (default: 7d)"`
	Limit  int    `json:"limit,omitempty" jsonschema:"Maximum flagged rules to return, worst first (default: 20, range: 1-100)"`
}

// The API caps a request at 31 days, so both windows are sent as they are.
var hygieneWindowDurations = map[string]time.Duration{
	hygieneWindow7d:  7 * 24 * time.Hour,
	hygieneWindow30d: 30 * 24 * time.Hour,
}

const (
	hygieneWindow7d  = "7d"
	hygieneWindow30d = "30d"
)

type alertHygieneResult struct {
	Window   string                 `json:"window"`
	From     string                 `json:"from"`
	To       string                 `json:"to"`
	Coverage alerts.HygieneCoverage `json:"coverage"`
	// UpstreamTruncated: the API capped its list by fire count, so the quietest
	// rules (the likeliest stale ones) were not evaluated.
	UpstreamTruncated bool               `json:"upstream_truncated"`
	RulesEvaluated    int                `json:"rules_evaluated"`
	FlaggedRules      int                `json:"flagged_rules"`
	Truncated         bool               `json:"truncated"`
	OmittedForSize    int                `json:"omitted_for_size"`
	Unavailable       hygieneUnavailable `json:"unavailable"`
	Rules             []hygieneVerdict   `json:"rules"`
}

func NewGetAlertHygieneHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, GetAlertHygieneArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args GetAlertHygieneArgs) (*mcp.CallToolResult, any, error) {
		window, limit, err := parseAlertHygieneArgs(args)
		if err != nil {
			return nil, nil, err
		}

		to := utils.Now().UTC().Truncate(time.Minute)
		resp, err := alerts.GetAlertHygiene(ctx, last9api.NewClient(client, cfg), alerts.GetAlertHygieneInput{
			From: to.Add(-hygieneWindowDurations[window]), To: to, Timezone: "UTC",
		})
		var upstream *utils.UpstreamHTTPError
		if errors.As(err, &upstream) && upstream.StatusCode == http.StatusNotFound {
			return nil, nil, fmt.Errorf("%w This Last9 deployment may not serve alert hygiene yet; no alert rules were checked.", err)
		}
		if err != nil {
			return nil, nil, err
		}
		// The hygiene API sends stats only; fixes need each rule's condition and eval_window.
		configs, err := fetchAlertConfig(ctx, client, cfg)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to fetch alert rule configuration: %w", err)
		}

		rules := joinHygieneRuleConfig(resp.Rules, configs)
		ranked := rankAlertHygiene(rules, newHygieneWindow(window, resp))
		result := alertHygieneResult{
			Window:            window,
			From:              formatHygieneTime(resp.Period.From),
			To:                formatHygieneTime(resp.Period.To),
			Coverage:          resp.Coverage,
			UpstreamTruncated: resp.Truncated,
			RulesEvaluated:    len(resp.Rules),
			FlaggedRules:      len(ranked),
			Truncated:         len(ranked) > limit,
			Unavailable:       countUnavailable(rules),
			Rules:             ranked[:min(limit, len(ranked))],
		}
		payload, err := marshalHygieneResult(result)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to marshal response: %w", err)
		}

		return &mcp.CallToolResult{
			Meta:    deeplink.ToMeta(deeplink.NewBuilder(cfg.OrgSlug, cfg.ClusterID).BuildAlertingGroupsLink()),
			Content: []mcp.Content{&mcp.TextContent{Text: string(payload)}},
		}, nil, nil
	}
}

// marshalHygieneResult keeps the longest prefix of the ranked rules that fits
// maxHygieneResponseBytes, at least one so the worst rule is always reported.
func marshalHygieneResult(result alertHygieneResult) ([]byte, error) {
	payload, err := json.Marshal(result)
	if err != nil || len(payload) <= maxHygieneResponseBytes {
		return payload, err
	}
	ranked := result.Rules
	// Size the rest of the result at its largest: truncated true and the most
	// rules omitted cannot render longer than this.
	result.Rules, result.Truncated, result.OmittedForSize = []hygieneVerdict{}, true, len(ranked)
	base, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	size, keep := len(base), 0
	for _, verdict := range ranked {
		rule, err := json.Marshal(verdict)
		if err != nil {
			return nil, err
		}
		if keep > 0 {
			size++ // comma
		}
		if size+len(rule) > maxHygieneResponseBytes && keep > 0 {
			break
		}
		size += len(rule)
		keep++
	}
	result.Rules, result.OmittedForSize = ranked[:keep], len(ranked)-keep
	return json.Marshal(result)
}

func parseAlertHygieneArgs(args GetAlertHygieneArgs) (string, int, error) {
	window := strings.ToLower(strings.TrimSpace(args.Window))
	switch window {
	case "":
		window = hygieneWindow7d
	case hygieneWindow7d, hygieneWindow30d:
	default:
		return "", 0, fmt.Errorf("window must be %q or %q, got %q", hygieneWindow7d, hygieneWindow30d, args.Window)
	}

	limit := args.Limit
	if limit == 0 {
		limit = defaultHygieneLimit
	}
	if limit < 1 || limit > maxHygieneLimit {
		return "", 0, fmt.Errorf("limit must be between 1 and %d, got %d", maxHygieneLimit, args.Limit)
	}
	return window, limit, nil
}
