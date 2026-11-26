package alerts

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"last9-mcp/internal/last9api"
)

const pathHygiene = "/alerts/hygiene"

const opHygiene = "alert hygiene"

const hygieneLabelPrefix = "label."

// The API returns at most 200 rules, each with its group labels.
const maxHygieneBytes = 4 << 20

type GetHygieneInput struct {
	From, To   time.Time
	Timezone   string
	Services   []string
	Severities []string
	// Sent as label.<key>=<value>: only rules whose alert group carries every label.
	Labels  map[string]string
	GroupBy string
}

// Counts are HealthMetric and derived values HygieneValue, each meaningful only with its
// status. Routed is omitted when the API has no delivery coverage, never false.
type HygieneRule struct {
	AlertGroupID           string            `json:"alert_group_id"`
	RuleID                 string            `json:"rule_id"`
	RuleType               string            `json:"rule_type"`
	RuleName               string            `json:"rule_name"`
	Service                string            `json:"service"`
	Severity               string            `json:"severity"`
	GroupLabels            map[string]string `json:"group_labels,omitempty"`
	FireCount              *HealthMetric     `json:"fire_count"`
	FlapCount              *HealthMetric     `json:"flap_count"`
	FlapsPerDay            *HygieneValue     `json:"flaps_per_day"`
	TimeFiringSeconds      *HealthMetric     `json:"time_firing_seconds"`
	LastFiredAt            *time.Time        `json:"last_fired_at,omitempty"`
	SecondsSinceLastFire   *int64            `json:"seconds_since_last_fire,omitempty"`
	AckRate                *HygieneValue     `json:"ack_rate"`
	MedianTimeToAckSeconds *HygieneValue     `json:"median_time_to_ack_seconds"`
	NotificationAttempts   *HealthMetric     `json:"notification_attempts"`
	Routed                 *bool             `json:"routed,omitempty"`
}

type HygieneValue struct {
	Status string   `json:"status"`
	Value  *float64 `json:"value,omitempty"`
	Reason string   `json:"reason,omitempty"`
}

type GetHygieneResponse struct {
	Coverage json.RawMessage `json:"coverage"`
	Rules    []HygieneRule   `json:"rules"`
	// True when the API cut the rule list at its cap; the rules kept are the top by fire count.
	Truncated bool            `json:"truncated"`
	GroupBy   string          `json:"group_by,omitempty"`
	Groups    json.RawMessage `json:"groups,omitempty"`
}

// GetHygiene calls GET /alerts/hygiene.
func GetHygiene(ctx context.Context, c *last9api.Client, in GetHygieneInput) (GetHygieneResponse, error) {
	var resp GetHygieneResponse
	err := queryHygiene(ctx, c, in, &resp)
	if err != nil {
		return resp, err
	}
	// Do accepts an empty 2xx; a missing rules list must not read as "no noisy rules".
	if resp.Rules == nil {
		return resp, fmt.Errorf("%s: response has no rules list", opHygiene)
	}
	return resp, nil
}

// Values of HealthMetric.Status, HygieneValue.Status and HygieneCoverageSource.Status.
const (
	HygieneStatusAvailable   = "available"
	HygieneStatusPartial     = "partial"
	HygieneStatusUnavailable = "unavailable"
	HygieneStatusComplete    = "complete"
)

// GetAlertHygieneInput is a half-open [From, To) range; the API caps it at 31 days.
type GetAlertHygieneInput struct {
	From, To time.Time
	Timezone string
}

type HygienePeriod struct {
	From     time.Time `json:"from"`
	To       time.Time `json:"to"`
	Timezone string    `json:"timezone"`
}

// HygieneCoverageSource says how much of the period one source observed.
type HygieneCoverageSource struct {
	Status string        `json:"status"`
	Range  *HygieneRange `json:"range,omitempty"`
	Reason string        `json:"reason,omitempty"`
}

type HygieneRange struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}

// HygieneCoverage: lifecycle backs fire, flap and last-fired values; delivery backs routed.
type HygieneCoverage struct {
	Lifecycle HygieneCoverageSource `json:"lifecycle"`
	Delivery  HygieneCoverageSource `json:"delivery"`
}

// GetAlertHygieneResponse lists rules by fire count; Truncated is set when the API
// capped the list, so the quietest rules are missing from Rules.
type GetAlertHygieneResponse struct {
	Period    HygienePeriod   `json:"period"`
	Coverage  HygieneCoverage `json:"coverage"`
	Rules     []HygieneRule   `json:"rules"`
	Truncated bool            `json:"truncated"`
}

// GetAlertHygiene calls GET /alerts/hygiene?from&to&timezone.
func GetAlertHygiene(ctx context.Context, c *last9api.Client, in GetAlertHygieneInput) (*GetAlertHygieneResponse, error) {
	var out GetAlertHygieneResponse
	if err := queryHygiene(ctx, c, GetHygieneInput{From: in.From, To: in.To, Timezone: in.Timezone}, &out); err != nil {
		return nil, err
	}
	if out.Rules == nil {
		return nil, fmt.Errorf("%s: response has no rules list", opHygiene)
	}
	return &out, nil
}

// queryHygiene calls GET /alerts/hygiene.
func queryHygiene(ctx context.Context, c *last9api.Client, in GetHygieneInput, out any) error {
	q := url.Values{}
	q.Set("from", in.From.UTC().Format(time.RFC3339Nano))
	q.Set("to", in.To.UTC().Format(time.RFC3339Nano))
	if in.Timezone != "" {
		q.Set("timezone", in.Timezone)
	}
	for _, s := range in.Services {
		q.Add("services", s)
	}
	for _, s := range in.Severities {
		q.Add("severities", s)
	}
	for k, v := range in.Labels {
		q.Set(hygieneLabelPrefix+k, v)
	}
	if in.GroupBy != "" {
		q.Set("group_by", in.GroupBy)
	}
	return c.Do(ctx, opHygiene, last9api.Request{Method: http.MethodGet, Path: pathHygiene}, out,
		last9api.WithQuery(q), last9api.WithMaxResponseBytes(maxHygieneBytes))
}
