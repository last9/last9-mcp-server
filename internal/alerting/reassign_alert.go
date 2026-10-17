package alerting

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"last9-mcp/internal/last9api"
	"last9-mcp/internal/last9api/alerts"
	"last9-mcp/internal/last9api/users"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type ReassignAlertArgs struct {
	AlertGroupID string `json:"alert_group_id" jsonschema:"(Required) Alert group id; copy from the alert in get_alerts"`
	RuleID       string `json:"rule_id" jsonschema:"(Required) Alert rule id; copy from the alert in get_alerts"`
	RuleType     string `json:"rule_type" jsonschema:"(Required) Alert rule type; copy from the alert in get_alerts"`
	OwnerEmail   string `json:"owner_email,omitempty" jsonschema:"Email of the new owner (preferred); matched case-insensitively. Provide exactly one of owner_email or owner_user_id"`
	OwnerUserID  string `json:"owner_user_id,omitempty" jsonschema:"User id of the new owner. Provide exactly one of owner_email or owner_user_id"`
}

func NewReassignAlertHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, ReassignAlertArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args ReassignAlertArgs) (*mcp.CallToolResult, any, error) {
		if strings.TrimSpace(args.AlertGroupID) == "" || strings.TrimSpace(args.RuleID) == "" || strings.TrimSpace(args.RuleType) == "" {
			return utils.ToolErrorResult("alert_group_id, rule_id and rule_type are required; copy them from the alert in get_alerts"), nil, nil
		}
		email, ownerID := strings.TrimSpace(args.OwnerEmail), strings.TrimSpace(args.OwnerUserID)
		if (email == "") == (ownerID == "") {
			return utils.ToolErrorResult("provide exactly one of owner_email or owner_user_id"), nil, nil
		}
		api := last9api.NewClient(client, cfg)
		if email != "" {
			list, err := users.ListUsers(ctx, api)
			if err != nil {
				return nil, nil, err
			}
			for _, u := range list {
				if u.DeletedAt == nil && strings.EqualFold(u.Email, email) {
					ownerID = u.ID
					break
				}
			}
			if ownerID == "" {
				// Name an unknown alert before the email: it is the more basic mistake.
				if refusal, err := requireRecentAlert(ctx, api, alertEpisode{AlertGroupID: args.AlertGroupID, RuleID: args.RuleID, RuleType: args.RuleType}); refusal != nil || err != nil {
					return refusal, nil, err
				}
				return utils.ToolErrorResult("no active user with that email"), nil, nil
			}
		}
		claim, err := alerts.ReassignAlertEpisode(ctx, api, alerts.ReassignAlertEpisodeInput{
			AlertGroupID: args.AlertGroupID, RuleID: args.RuleID, RuleType: args.RuleType, OwnerUserID: ownerID,
		})
		if err != nil {
			return reassignErrorResult(ctx, api, err, alertEpisode{AlertGroupID: args.AlertGroupID, RuleID: args.RuleID, RuleType: args.RuleType})
		}
		raw, err := json.Marshal(claim)
		if err != nil {
			return nil, nil, err
		}
		return alertMutationResult(cfg, raw, ""), nil, nil
	}
}

// The API answers 404 both for an unclaimed alert and for ids that match no alert, so a 404
// asks the monitor which one it is.
func reassignErrorResult(ctx context.Context, api *last9api.Client, err error, episode alertEpisode) (*mcp.CallToolResult, any, error) {
	var upstream *utils.UpstreamHTTPError
	if !errors.As(err, &upstream) {
		return nil, nil, err
	}
	switch {
	case upstream.StatusCode == http.StatusNotFound:
		if refusal, err := requireRecentAlert(ctx, api, episode); refusal != nil || err != nil {
			return refusal, nil, err
		}
		return utils.ToolErrorResult("this alert has no owner yet; it must be acknowledged (acknowledge_alert) before it can be reassigned"), nil, nil
	case upstream.StatusCode == http.StatusForbidden:
		return utils.ToolErrorResult("reassigning requires a write-scope token and being an org admin or the alert's current owner"), nil, nil
	case upstream.StatusCode == http.StatusBadRequest && strings.Contains(upstream.Body, "invalid_target_owner"):
		return utils.ToolErrorResult("that user cannot respond to alerts (needs editor or admin role)"), nil, nil
	}
	return nil, nil, withUpstreamBody(err)
}
