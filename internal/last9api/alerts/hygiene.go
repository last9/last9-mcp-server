package alerts

import (
	"context"
	"net/http"
	"net/url"

	"last9-mcp/internal/last9api"
)

const pathAlertHygiene = "/alert-hygiene"

const opAlertHygiene = "alert hygiene"

const (
	HygieneWindow7d  = "7d"
	HygieneWindow30d = "30d"
)

type GetAlertHygieneInput struct {
	Window string
}

// Nullable fields are pointers: null or absent means no history (or, for routed, no
// routing data) was recorded, never zero or false.
type HygieneRule struct {
	RuleID         string           `json:"rule_id"`
	RuleName       string           `json:"rule_name"`
	AlertGroupID   string           `json:"alert_group_id"`
	AlertGroupName string           `json:"alert_group_name"`
	Algorithm      string           `json:"algorithm"`
	Expression     string           `json:"expression"`
	EvalWindow     int64            `json:"eval_window"`
	AlertCondition string           `json:"alert_condition"`
	Severity       string           `json:"severity"`
	IsDisabled     bool             `json:"is_disabled"`
	FireCount      int64            `json:"fire_count"`
	FlapsPerDay    float64          `json:"flaps_per_day"`
	FiringSeconds  float64          `json:"firing_seconds"`
	LastFiredAt    *int64           `json:"last_fired_at"`
	Ack            *HygieneAck      `json:"ack"`
	MutedSeconds   *float64         `json:"muted_seconds"`
	HistorySince   *int64           `json:"history_since"`
	Routed         *bool            `json:"routed"`
	Channels       []HygieneChannel `json:"channels"`
}

type HygieneAck struct {
	AckedEpisodes      int64    `json:"acked_episodes"`
	AckRate            float64  `json:"ack_rate"`
	MedianSecondsToAck *float64 `json:"median_seconds_to_ack"`
}

type HygieneChannel struct {
	Type        string `json:"type"`
	Destination string `json:"destination"`
}

// From, To, LastFiredAt and HistorySince are unix seconds.
type GetAlertHygieneResponse struct {
	Window string        `json:"window"`
	From   int64         `json:"from"`
	To     int64         `json:"to"`
	Rules  []HygieneRule `json:"rules"`
}

// GetAlertHygiene calls GET /alert-hygiene?window={window}.
func GetAlertHygiene(
	ctx context.Context, c *last9api.Client, in GetAlertHygieneInput,
) (*GetAlertHygieneResponse, error) {
	var out GetAlertHygieneResponse
	err := c.Do(ctx, opAlertHygiene,
		last9api.Request{Method: http.MethodGet, Path: pathAlertHygiene},
		&out,
		last9api.WithQuery(url.Values{"window": {in.Window}}),
	)
	if err != nil {
		return nil, err
	}
	return &out, nil
}
