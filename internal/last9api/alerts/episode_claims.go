package alerts

import (
	"context"
	"net/http"

	"last9-mcp/internal/last9api"
)

const pathAlertEpisodeClaimsReassign = "/alert-episode-claims/reassign"

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
