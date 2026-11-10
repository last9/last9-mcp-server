package alerts

import (
	"context"
	"net/http"

	"last9-mcp/internal/last9api"
)

const (
	pathAlertEpisodeClaimsClaim    = "/alert-episode-claims/claim"
	pathAlertEpisodeClaimsUnack    = "/alert-episode-claims/unack"
	pathAlertEpisodeClaimsReassign = "/alert-episode-claims/reassign"
)

type ClaimAlertEpisodeInput struct {
	AlertGroupID string `json:"alert_group_id"`
	RuleID       string `json:"rule_id"`
	RuleType     string `json:"rule_type"`
	Since        int64  `json:"since,omitempty"`
	MuteUntil    int64  `json:"mute_until,omitempty"`
}

type UnackAlertEpisodeInput struct {
	AlertGroupID string `json:"alert_group_id"`
	RuleID       string `json:"rule_id"`
	RuleType     string `json:"rule_type"`
}

type ReassignAlertEpisodeInput struct {
	AlertGroupID string `json:"alert_group_id"`
	RuleID       string `json:"rule_id"`
	RuleType     string `json:"rule_type"`
	OwnerUserID  string `json:"owner_user_id"`
}

type AlertEpisodeClaim struct {
	OwnerUserID     string `json:"owner_user_id"`
	OwnerName       string `json:"owner_name,omitempty"`
	OwnerEmail      string `json:"owner_email,omitempty"`
	ClaimedByUserID string `json:"claimed_by_user_id"`
	ClaimedAt       int64  `json:"claimed_at"`
	MutedUntil      int64  `json:"muted_until"`
}

// ReassignAlertEpisode calls POST /alert-episode-claims/reassign.
func ReassignAlertEpisode(ctx context.Context, c *last9api.Client, in ReassignAlertEpisodeInput) (*AlertEpisodeClaim, error) {
	var out AlertEpisodeClaim
	err := c.Do(ctx, "reassign alert",
		last9api.Request{Method: http.MethodPost, Path: pathAlertEpisodeClaimsReassign, Body: in}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// ClaimAlertEpisode calls POST /alert-episode-claims/claim.
func ClaimAlertEpisode(ctx context.Context, c *last9api.Client, in ClaimAlertEpisodeInput) (*AlertEpisodeClaim, error) {
	var out AlertEpisodeClaim
	err := c.Do(ctx, "acknowledge alert",
		last9api.Request{Method: http.MethodPost, Path: pathAlertEpisodeClaimsClaim, Body: in}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// UnackAlertEpisode calls POST /alert-episode-claims/unack.
func UnackAlertEpisode(ctx context.Context, c *last9api.Client, in UnackAlertEpisodeInput) error {
	return c.Do(ctx, "unacknowledge alert",
		last9api.Request{Method: http.MethodPost, Path: pathAlertEpisodeClaimsUnack, Body: in}, nil)
}
