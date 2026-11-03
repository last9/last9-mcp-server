package alerts

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"last9-mcp/internal/last9api"
)

const pathAlertHygiene = "/alerts/hygiene"

const opAlertHygiene = "alert hygiene"

// Values of HygieneMetric.Status, HygieneValue.Status and HygieneCoverageSource.Status.
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

// HygieneMetric is a count the API may not have observed: Count is nil when Status
// is unavailable, so an unobserved value never reads as zero.
type HygieneMetric struct {
	Status string `json:"status"`
	Count  *int64 `json:"count,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// HygieneValue is a derived value with the same availability contract as HygieneMetric.
type HygieneValue struct {
	Status string   `json:"status"`
	Value  *float64 `json:"value,omitempty"`
	Reason string   `json:"reason,omitempty"`
}

// HygieneRule is one rule's stats for the period, without mute history or rule
// configuration. LastFiredAt is nil without lifecycle coverage and may precede the
// period for a still-open episode; Routed is nil without delivery coverage.
type HygieneRule struct {
	AlertGroupID           string            `json:"alert_group_id"`
	RuleID                 string            `json:"rule_id"`
	RuleType               string            `json:"rule_type"`
	RuleName               string            `json:"rule_name"`
	Service                string            `json:"service,omitempty"`
	Severity               string            `json:"severity,omitempty"`
	GroupLabels            map[string]string `json:"group_labels,omitempty"`
	FireCount              HygieneMetric     `json:"fire_count"`
	FlapCount              HygieneMetric     `json:"flap_count"`
	FlapsPerDay            HygieneValue      `json:"flaps_per_day"`
	TimeFiringSeconds      HygieneMetric     `json:"time_firing_seconds"`
	LastFiredAt            *time.Time        `json:"last_fired_at,omitempty"`
	AckRate                HygieneValue      `json:"ack_rate"`
	MedianTimeToAckSeconds HygieneValue      `json:"median_time_to_ack_seconds"`
	Routed                 *bool             `json:"routed,omitempty"`
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
func GetAlertHygiene(
	ctx context.Context, c *last9api.Client, in GetAlertHygieneInput,
) (*GetAlertHygieneResponse, error) {
	var out GetAlertHygieneResponse
	err := c.Do(ctx, opAlertHygiene,
		last9api.Request{Method: http.MethodGet, Path: pathAlertHygiene},
		&out,
		last9api.WithQuery(url.Values{
			"from":     {in.From.UTC().Format(time.RFC3339)},
			"to":       {in.To.UTC().Format(time.RFC3339)},
			"timezone": {in.Timezone},
		}),
	)
	if err != nil {
		return nil, err
	}
	// Do accepts an empty 2xx; a missing rules list must not read as "every rule is fine".
	if out.Rules == nil {
		return nil, fmt.Errorf("%s: response has no rules list", opAlertHygiene)
	}
	return &out, nil
}
