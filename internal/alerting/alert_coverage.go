package alerting

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"last9-mcp/internal/deeplink"
	"last9-mcp/internal/last9api"
	"last9-mcp/internal/last9api/alerts"
	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const defaultCoverageMutedDays = 7

// The API rejects larger values with a 400.
const maxCoverageMutedDays = 3650

// Kinds outside this list are still rendered, after these, so a gap kind the API adds
// later is never dropped.
var coverageKindOrder = []string{
	alerts.CoverageGapUncoveredSignal,
	alerts.CoverageGapAllRulesDisabled,
	alerts.CoverageGapNoNotificationChannel,
	alerts.CoverageGapMutedTooLong,
}

type GetAlertCoverageArgs struct {
	Service     string `json:"service,omitempty" jsonschema:"Service name to check (optional)"`
	Environment string `json:"environment,omitempty" jsonschema:"Deployment environment to check, e.g. prod (optional)"`
	EntityID    string `json:"entity_id,omitempty" jsonschema:"Alert group (entity) id from get_alert_groups (optional)"`
	MutedDays   int    `json:"muted_days,omitempty" jsonschema:"A rule muted for more than this many days is a gap (default: 7, minimum: 1, maximum: 3650)"`
}

type alertCoverageScope struct {
	Service     string `json:"service,omitempty"`
	Environment string `json:"environment,omitempty"`
	EntityID    string `json:"entity_id,omitempty"`
	MutedDays   int    `json:"muted_days"`
}

type alertCoverageResult struct {
	Scope       alertCoverageScope   `json:"scope"`
	GeneratedAt string               `json:"generated_at,omitempty"`
	Complete    bool                 `json:"complete"`
	Summary     string               `json:"summary"`
	Counts      map[string]int       `json:"counts"`
	Groups      []alertCoverageGroup `json:"groups"`
}

type alertCoverageGroup struct {
	Kind  string             `json:"kind"`
	Count int                `json:"count"`
	Gaps  []alertCoverageGap `json:"gaps"`
}

type alertCoverageGap struct {
	EntityName    string            `json:"entity_name,omitempty"`
	Service       string            `json:"service,omitempty"`
	RuleName      string            `json:"rule_name,omitempty"`
	Severity      string            `json:"severity,omitempty"`
	MutedSince    string            `json:"muted_since,omitempty"`
	MuteUntil     string            `json:"mute_until,omitempty"`
	Detail        string            `json:"detail,omitempty"`
	SuggestedTool string            `json:"suggested_tool,omitempty"`
	SuggestedArgs map[string]string `json:"suggested_args,omitempty"`
}

func NewGetAlertCoverageHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, GetAlertCoverageArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args GetAlertCoverageArgs) (*mcp.CallToolResult, any, error) {
		in, err := parseAlertCoverageArgs(args)
		if err != nil {
			return nil, nil, err
		}

		resp, err := alerts.GetAlertCoverage(ctx, last9api.NewClient(client, cfg), in)
		if err != nil {
			return nil, nil, err
		}

		payload, err := json.Marshal(buildAlertCoverageResult(in, resp))
		if err != nil {
			return nil, nil, fmt.Errorf("failed to marshal response: %w", err)
		}
		return &mcp.CallToolResult{
			Meta:    deeplink.ToMeta(deeplink.NewBuilder(cfg.OrgSlug, cfg.ClusterID).BuildAlertingGroupsLink()),
			Content: []mcp.Content{&mcp.TextContent{Text: string(payload)}},
		}, nil, nil
	}
}

func parseAlertCoverageArgs(args GetAlertCoverageArgs) (alerts.GetAlertCoverageInput, error) {
	mutedDays := args.MutedDays
	if mutedDays == 0 {
		mutedDays = defaultCoverageMutedDays
	}
	if mutedDays < 1 || mutedDays > maxCoverageMutedDays {
		return alerts.GetAlertCoverageInput{}, fmt.Errorf("muted_days must be between 1 and %d, got %d", maxCoverageMutedDays, args.MutedDays)
	}
	return alerts.GetAlertCoverageInput{
		Service:     strings.TrimSpace(args.Service),
		Environment: strings.TrimSpace(args.Environment),
		EntityID:    strings.TrimSpace(args.EntityID),
		MutedDays:   mutedDays,
	}, nil
}

func buildAlertCoverageResult(in alerts.GetAlertCoverageInput, resp *alerts.GetAlertCoverageResponse) alertCoverageResult {
	scope := alertCoverageScope{
		Service:     in.Service,
		Environment: in.Environment,
		EntityID:    in.EntityID,
		MutedDays:   in.MutedDays,
	}
	if resp.MutedDays > 0 {
		scope.MutedDays = resp.MutedDays
	}
	counts := resp.Counts
	if counts == nil {
		counts = map[string]int{}
	}
	result := alertCoverageResult{
		Scope:       scope,
		GeneratedAt: formatCoverageTime(resp.GeneratedAt),
		Counts:      counts,
		Groups:      groupCoverageGaps(resp.Gaps),
	}

	counted := 0
	for _, n := range counts {
		counted += n
	}
	switch {
	case len(resp.Gaps) == 0 && counted == 0:
		result.Complete = true
		result.Summary = fmt.Sprintf("Alert coverage is complete for %s: no uncovered signals, fully disabled alert groups, unrouted alert groups, or rules muted for more than %d days.",
			describeCoverageScope(scope), scope.MutedDays)
	case len(resp.Gaps) == 0:
		result.Summary = fmt.Sprintf("The API counted %d coverage gaps for %s but listed none; coverage is not confirmed complete.",
			counted, describeCoverageScope(scope))
	default:
		result.Summary = fmt.Sprintf("%d alert coverage gaps for %s.", len(resp.Gaps), describeCoverageScope(scope))
	}
	return result
}

func groupCoverageGaps(gaps []alerts.CoverageGap) []alertCoverageGroup {
	byKind := map[string]*alertCoverageGroup{}
	var unknown []string
	for _, gap := range gaps {
		group, ok := byKind[gap.Kind]
		if !ok {
			group = &alertCoverageGroup{Kind: gap.Kind}
			byKind[gap.Kind] = group
			if !slices.Contains(coverageKindOrder, gap.Kind) {
				unknown = append(unknown, gap.Kind)
			}
		}
		group.Gaps = append(group.Gaps, projectCoverageGap(gap))
		group.Count++
	}
	slices.Sort(unknown)

	groups := make([]alertCoverageGroup, 0, len(byKind))
	for _, kind := range append(slices.Clone(coverageKindOrder), unknown...) {
		if group, ok := byKind[kind]; ok {
			groups = append(groups, *group)
		}
	}
	return groups
}

func projectCoverageGap(gap alerts.CoverageGap) alertCoverageGap {
	suggestedArgs := map[string]string{}
	for key, value := range map[string]string{
		"entity_id": gap.EntityID,
		"rule_id":   gap.RuleID,
		"signal":    gap.Signal,
	} {
		if value != "" {
			suggestedArgs[key] = value
		}
	}
	return alertCoverageGap{
		EntityName:    gap.EntityName,
		Service:       gap.Service,
		RuleName:      gap.RuleName,
		Severity:      gap.Severity,
		MutedSince:    formatCoverageTime(gap.MutedSince),
		MuteUntil:     formatCoverageTime(gap.MuteUntil),
		Detail:        gap.Detail,
		SuggestedTool: gap.SuggestedTool,
		SuggestedArgs: suggestedArgs,
	}
}

func describeCoverageScope(scope alertCoverageScope) string {
	var parts []string
	if scope.Service != "" {
		parts = append(parts, fmt.Sprintf("service %q", scope.Service))
	}
	if scope.Environment != "" {
		parts = append(parts, fmt.Sprintf("environment %q", scope.Environment))
	}
	if scope.EntityID != "" {
		parts = append(parts, fmt.Sprintf("entity_id %q", scope.EntityID))
	}
	if len(parts) == 0 {
		return "the whole organization"
	}
	return strings.Join(parts, ", ")
}

func formatCoverageTime(unix int64) string {
	if unix <= 0 {
		return ""
	}
	return time.Unix(unix, 0).UTC().Format(time.RFC3339)
}
