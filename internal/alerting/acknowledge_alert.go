package alerting

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"last9-mcp/internal/last9api"
	"last9-mcp/internal/last9api/alerts"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type AcknowledgeAlertArgs struct {
	AlertGroupID string `json:"alert_group_id" jsonschema:"(Required) Alert group id; copy from the alert in get_alerts"`
	RuleID       string `json:"rule_id" jsonschema:"(Required) Alert rule id; copy from the alert in get_alerts"`
	RuleType     string `json:"rule_type" jsonschema:"(Required) Alert rule type; copy from the alert in get_alerts"`
	Action       string `json:"action,omitempty" jsonschema:"ack (default) takes ownership; unack releases it. Only ack accepts mute_until"`
	MuteUntil    int64  `json:"mute_until,omitempty" jsonschema:"Unix epoch seconds of a mute already applied to this alert; recorded on the claim, does not mute. Omit or 0 for none. Only with action ack"`
}

func NewAcknowledgeAlertHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, AcknowledgeAlertArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args AcknowledgeAlertArgs) (*mcp.CallToolResult, any, error) {
		if strings.TrimSpace(args.AlertGroupID) == "" || strings.TrimSpace(args.RuleID) == "" || strings.TrimSpace(args.RuleType) == "" {
			return utils.ToolErrorResult("alert_group_id, rule_id and rule_type are required; copy them from the alert in get_alerts"), nil, nil
		}
		action := strings.ToLower(strings.TrimSpace(args.Action))
		if action == "" {
			action = "ack"
		}
		if action != "ack" && action != "unack" {
			return utils.ToolErrorResult("action must be ack or unack"), nil, nil
		}
		if args.MuteUntil < 0 {
			return utils.ToolErrorResult("mute_until must be a Unix epoch in seconds, or 0/omitted for none"), nil, nil
		}
		if action == "unack" && args.MuteUntil != 0 {
			return utils.ToolErrorResult("mute_until applies only to action ack"), nil, nil
		}
		api := last9api.NewClient(client, cfg)
		if action == "unack" {
			err := alerts.UnackAlertEpisode(ctx, api, alerts.UnackAlertEpisodeInput{
				AlertGroupID: args.AlertGroupID, RuleID: args.RuleID, RuleType: args.RuleType,
			})
			if err != nil {
				return acknowledgeErrorResult(err, action)
			}
			return alertMutationResult(cfg, nil, `{"released":true}`), nil, nil
		}
		if refusal, err := requireRecentAlert(ctx, api, alertEpisode{AlertGroupID: args.AlertGroupID, RuleID: args.RuleID, RuleType: args.RuleType}); refusal != nil || err != nil {
			return refusal, nil, err
		}
		claim, err := alerts.ClaimAlertEpisode(ctx, api, alerts.ClaimAlertEpisodeInput{
			AlertGroupID: args.AlertGroupID, RuleID: args.RuleID, RuleType: args.RuleType, MuteUntil: args.MuteUntil,
		})
		if err != nil {
			return acknowledgeErrorResult(err, action)
		}
		raw, err := json.Marshal(claim)
		if err != nil {
			return nil, nil, err
		}
		return alertMutationResult(cfg, raw, ""), nil, nil
	}
}

func acknowledgeErrorResult(err error, action string) (*mcp.CallToolResult, any, error) {
	var upstream *utils.UpstreamHTTPError
	if !errors.As(err, &upstream) {
		return nil, nil, err
	}
	switch {
	case upstream.StatusCode == http.StatusConflict && action == "ack":
		return utils.ToolErrorResult("this alert is already acknowledged by another user; check the claim field on the alert in get_alerts for the owner. Only that owner or an org admin can hand it off with reassign_alert"), nil, nil
	case upstream.StatusCode == http.StatusForbidden && action == "ack":
		return utils.ToolErrorResult("acknowledging requires a write-scope token whose owner has the editor or admin role (viewers only if the org enables viewer acknowledgement)"), nil, nil
	case upstream.StatusCode == http.StatusForbidden:
		return utils.ToolErrorResult("releasing requires a write-scope token and being an org admin or the alert's current owner"), nil, nil
	case upstream.StatusCode == http.StatusNotFound && action == "unack":
		return utils.ToolErrorResult("this alert has no acknowledgement to release; it may already be unacknowledged or resolved. Re-run get_alerts to check"), nil, nil
	case upstream.StatusCode == http.StatusNotFound:
		return utils.ToolErrorResult("the acknowledgement disappeared while updating (the alert likely resolved); re-run get_alerts and retry if it is still firing"), nil, nil
	}
	return nil, nil, withUpstreamBody(err)
}
