package alerts

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"last9-mcp/internal/last9api"
)

const pathHygiene = "/alerts/hygiene"

const opHygiene = "alert hygiene"

// One row per alert rule in the organization.
const maxHygieneBytes = 4 << 20

type GetHygieneInput struct {
	// Unix seconds.
	From, To   int64
	Timezone   string
	Services   []string
	Severities []string
}

// Nullable fields are pointers: null or absent means no history (or, for routed, no
// routing data) was recorded, never zero or false.
type HygieneRule struct {
	RuleID         string      `json:"rule_id"`
	RuleName       string      `json:"rule_name"`
	AlertGroupName string      `json:"alert_group_name,omitempty"`
	Severity       string      `json:"severity,omitempty"`
	FireCount      *int64      `json:"fire_count"`
	FlapsPerDay    *float64    `json:"flaps_per_day"`
	FiringSeconds  *float64    `json:"firing_seconds"`
	Ack            *HygieneAck `json:"ack"`
	Routed         *bool       `json:"routed"`
}

type HygieneAck struct {
	AckRate            *float64 `json:"ack_rate"`
	MedianSecondsToAck *float64 `json:"median_seconds_to_ack"`
}

type GetHygieneResponse struct {
	Rules []HygieneRule `json:"rules"`
}

// GetHygiene calls GET /alerts/hygiene.
func GetHygiene(ctx context.Context, c *last9api.Client, in GetHygieneInput) (GetHygieneResponse, error) {
	q := url.Values{}
	q.Set("from", strconv.FormatInt(in.From, 10))
	q.Set("to", strconv.FormatInt(in.To, 10))
	if in.Timezone != "" {
		q.Set("timezone", in.Timezone)
	}
	for _, s := range in.Services {
		q.Add("services", s)
	}
	for _, s := range in.Severities {
		q.Add("severities", s)
	}
	var resp GetHygieneResponse
	err := c.Do(ctx, opHygiene, last9api.Request{Method: http.MethodGet, Path: pathHygiene}, &resp,
		last9api.WithQuery(q), last9api.WithMaxResponseBytes(maxHygieneBytes))
	if err != nil {
		return resp, err
	}
	// Do accepts an empty 2xx; a missing rules list must not read as "no noisy rules".
	if resp.Rules == nil {
		return resp, fmt.Errorf("%s: response has no rules list", opHygiene)
	}
	return resp, nil
}
