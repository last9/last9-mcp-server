package alerting

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"last9-mcp/internal/last9api"
	"last9-mcp/internal/last9api/alerts"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Matches the API's own cap, so an oversized window fails here with the reason.
const maxOpsSummaryWindow = 31 * 24 * time.Hour

const (
	opsRulesLimit     = 20
	flappingMinPerDay = 1.0
)

const (
	metricAvailable   = "available"
	metricPartial     = "partial"
	metricUnavailable = "unavailable"
)

type GetAlertOperationsSummaryArgs struct {
	StartTimeISO string   `json:"start_time_iso" jsonschema:"(Required) Start of the window, RFC3339 (e.g. 2026-10-16T00:00:00Z)"`
	EndTimeISO   string   `json:"end_time_iso" jsonschema:"(Required) End of the window, RFC3339; at most 31 days after start_time_iso"`
	Timezone     string   `json:"timezone,omitempty" jsonschema:"IANA timezone the API uses for day boundaries, e.g. Asia/Kolkata (default: UTC)"`
	Severities   []string `json:"severities,omitempty" jsonschema:"Only alerts with these severities, e.g. P1 (default: all)"`
	Services     []string `json:"services,omitempty" jsonschema:"Only alerts for these services (default: all)"`
	IncludeRules bool     `json:"include_rules,omitempty" jsonschema:"Also return per-rule hygiene: top 20 rules by fire_count, plus flapping and unrouted rules (default: false)"`
}

type opsSummaryWindow struct {
	StartTimeISO string `json:"start_time_iso"`
	EndTimeISO   string `json:"end_time_iso"`
	Timezone     string `json:"timezone"`
}

type opsDelivered struct {
	Total alerts.HealthMetric `json:"total"`
	Slack alerts.HealthMetric `json:"slack"`
	Email alerts.HealthMetric `json:"email"`
}

type opsRules struct {
	TotalRules     int                  `json:"total_rules"`
	TopByFireCount []alerts.HygieneRule `json:"top_by_fire_count"`
	Flapping       []alerts.HygieneRule `json:"flapping"`
	FlappingTotal  int                  `json:"flapping_total"`
	Unrouted       []alerts.HygieneRule `json:"unrouted"`
	UnroutedTotal  int                  `json:"unrouted_total"`
}

type opsError struct {
	Field string `json:"field"`
	Error string `json:"error"`
}

type alertOperationsSummaryResult struct {
	Window               opsSummaryWindow    `json:"window"`
	SchemaVersion        *int                `json:"schema_version"`
	AlertsRaised         alerts.HealthMetric `json:"alerts_raised"`
	Acknowledged         alerts.HealthMetric `json:"acknowledged"`
	Unacknowledged       alerts.HealthMetric `json:"unacknowledged"`
	CurrentBacklog       alerts.HealthMetric `json:"current_backlog"`
	Delivered            opsDelivered        `json:"delivered"`
	NotificationOutcomes json.RawMessage     `json:"notification_outcomes"`
	TopRules             json.RawMessage     `json:"top_rules"`
	NeedsAttention       json.RawMessage     `json:"needs_attention"`
	Coverage             json.RawMessage     `json:"coverage"`
	Rules                *opsRules           `json:"rules"`
	Errors               []opsError          `json:"errors,omitempty"`
}

func NewGetAlertOperationsSummaryHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, GetAlertOperationsSummaryArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args GetAlertOperationsSummaryArgs) (*mcp.CallToolResult, any, error) {
		in, window, msg := parseOpsSummaryArgs(args)
		if msg != "" {
			return utils.ToolErrorResult(msg), nil, nil
		}

		api := last9api.NewClient(client, cfg)
		summary, err := alerts.GetHealthSummary(ctx, api, in)
		if err != nil {
			return opsSummaryErrorResult(err)
		}
		result := shapeOpsSummary(window, summary)

		if args.IncludeRules {
			hygiene, err := alerts.GetHygiene(ctx, api, alerts.GetHygieneInput{
				From: in.From, To: in.To, Timezone: in.Timezone, Services: in.Services, Severities: in.Severities,
			})
			if err != nil {
				result.Errors = append(result.Errors, opsError{Field: "rules", Error: err.Error()})
			} else {
				result.Rules = shapeOpsRules(hygiene.Rules)
			}
		}

		body, err := json.Marshal(result)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to encode alert operations summary: %w", err)
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(body)}}}, nil, nil
	}
}

func parseOpsSummaryArgs(args GetAlertOperationsSummaryArgs) (alerts.GetHealthSummaryInput, opsSummaryWindow, string) {
	var in alerts.GetHealthSummaryInput
	start, msg := parseOpsTime("start_time_iso", args.StartTimeISO)
	if msg != "" {
		return in, opsSummaryWindow{}, msg
	}
	end, msg := parseOpsTime("end_time_iso", args.EndTimeISO)
	if msg != "" {
		return in, opsSummaryWindow{}, msg
	}
	if !start.Before(end) {
		return in, opsSummaryWindow{}, "end_time_iso must be after start_time_iso"
	}
	if end.Sub(start) > maxOpsSummaryWindow {
		return in, opsSummaryWindow{}, fmt.Sprintf("the window is %s; it can span at most 31 days, so split it into smaller windows", end.Sub(start))
	}
	tz := strings.TrimSpace(args.Timezone)
	if tz == "" {
		tz = "UTC"
	}
	// LoadLocation accepts "Local", which would mean this server's zone, not the user's.
	if _, err := time.LoadLocation(tz); err != nil || tz == "Local" {
		return in, opsSummaryWindow{}, fmt.Sprintf("timezone %q is not an IANA timezone name such as UTC or Asia/Kolkata", args.Timezone)
	}
	in = alerts.GetHealthSummaryInput{
		From: start.Unix(), To: end.Unix(), Timezone: tz,
		Services: nonEmptyTrimmed(args.Services), Severities: nonEmptyTrimmed(args.Severities),
	}
	window := opsSummaryWindow{
		StartTimeISO: start.UTC().Format(time.RFC3339),
		EndTimeISO:   end.UTC().Format(time.RFC3339),
		Timezone:     tz,
	}
	return in, window, ""
}

func parseOpsTime(name, value string) (time.Time, string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, name + " is required (RFC3339, e.g. 2026-10-16T00:00:00Z)"
	}
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, fmt.Sprintf("%s %q is not RFC3339, e.g. 2026-10-16T00:00:00Z", name, value)
	}
	return t, ""
}

func nonEmptyTrimmed(values []string) []string {
	var out []string
	for _, v := range values {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func opsSummaryErrorResult(err error) (*mcp.CallToolResult, any, error) {
	var apiErr *utils.UpstreamHTTPError
	if !errors.As(err, &apiErr) {
		return nil, nil, err
	}
	if apiErr.StatusCode == http.StatusNotFound {
		return utils.ToolErrorResult("the alert health summary is not available in this environment (HTTP 404); tell the user alert raise and acknowledgement history is unavailable here, and do not retry"), nil, nil
	}
	return utils.ToolErrorResult(apiErr.Error()), nil, nil
}

func shapeOpsSummary(window opsSummaryWindow, s alerts.GetHealthSummaryResponse) alertOperationsSummaryResult {
	var d alerts.HealthDelivered
	if s.Delivered != nil {
		d = *s.Delivered
	}
	return alertOperationsSummaryResult{
		Window:         window,
		SchemaVersion:  s.SchemaVersion,
		AlertsRaised:   normalizeHealthMetric(s.AlertsRaised),
		Acknowledged:   normalizeHealthMetric(s.Acknowledged),
		Unacknowledged: normalizeHealthMetric(s.Unacknowledged),
		CurrentBacklog: normalizeHealthMetric(s.CurrentBacklog),
		Delivered: opsDelivered{
			Total: normalizeHealthMetric(d.Total),
			Slack: normalizeHealthMetric(d.Slack),
			Email: normalizeHealthMetric(d.Email),
		},
		NotificationOutcomes: s.NotificationOutcomes,
		TopRules:             s.TopRules,
		NeedsAttention:       s.NeedsAttention,
		Coverage:             s.Coverage,
	}
}

// A count is served only under a status we know how to read; anything else is withheld
// so it can never be presented as a plain number.
func normalizeHealthMetric(m *alerts.HealthMetric) alerts.HealthMetric {
	if m == nil {
		return alerts.HealthMetric{Status: metricUnavailable, Reason: "not returned by the API"}
	}
	switch m.Status {
	case metricAvailable, metricPartial, metricUnavailable:
		return *m
	}
	return alerts.HealthMetric{Status: metricUnavailable, Reason: fmt.Sprintf("unrecognized status %q", m.Status)}
}

func shapeOpsRules(rules []alerts.HygieneRule) *opsRules {
	byFires := append([]alerts.HygieneRule(nil), rules...)
	sort.SliceStable(byFires, func(i, j int) bool { return fireCount(byFires[i]) > fireCount(byFires[j]) })

	var flapping, unrouted []alerts.HygieneRule
	for _, r := range byFires {
		if r.FlapsPerDay != nil && *r.FlapsPerDay >= flappingMinPerDay {
			flapping = append(flapping, r)
		}
		if r.Routed != nil && !*r.Routed {
			unrouted = append(unrouted, r)
		}
	}
	sort.SliceStable(flapping, func(i, j int) bool { return *flapping[i].FlapsPerDay > *flapping[j].FlapsPerDay })

	return &opsRules{
		TotalRules:     len(rules),
		TopByFireCount: firstRules(byFires),
		Flapping:       firstRules(flapping),
		FlappingTotal:  len(flapping),
		Unrouted:       firstRules(unrouted),
		UnroutedTotal:  len(unrouted),
	}
}

func fireCount(r alerts.HygieneRule) int64 {
	if r.FireCount == nil {
		return -1
	}
	return *r.FireCount
}

func firstRules(rules []alerts.HygieneRule) []alerts.HygieneRule {
	if len(rules) > opsRulesLimit {
		rules = rules[:opsRulesLimit]
	}
	if rules == nil {
		return []alerts.HygieneRule{}
	}
	return rules
}
