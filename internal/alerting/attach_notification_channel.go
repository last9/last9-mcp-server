package alerting

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"last9-mcp/internal/last9api"
	"last9-mcp/internal/last9api/notifications"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type AttachNotificationChannelArgs struct {
	ChannelID    int    `json:"channel_id" jsonschema:"(Required) Channel id from create_notification_channel or get_notification_channels with scope=channels"`
	AlertGroupID string `json:"alert_group_id" jsonschema:"(Required) Alert group id; copy from get_alert_groups"`
	Severity     string `json:"severity" jsonschema:"(Required) breach, threat, or both. both attaches the channel twice, once per severity"`
}

type notificationChannelAttachment struct {
	AttachmentID int    `json:"attachment_id"`
	AlertGroupID string `json:"alert_group_id"`
	Severity     string `json:"severity"`
}

func NewAttachNotificationChannelHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, AttachNotificationChannelArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args AttachNotificationChannelArgs) (*mcp.CallToolResult, any, error) {
		groupID, severity := strings.TrimSpace(args.AlertGroupID), strings.ToLower(strings.TrimSpace(args.Severity))
		if args.ChannelID <= 0 || groupID == "" {
			return utils.ToolErrorResult("channel_id and alert_group_id are required"), nil, nil
		}
		severities := []string{severity}
		switch severity {
		case "breach", "threat":
		case "both":
			severities = []string{"breach", "threat"}
		default:
			return utils.ToolErrorResult("severity must be breach, threat or both"), nil, nil
		}
		api := last9api.NewClient(client, cfg)
		property, found, err := notifications.ChannelProperty(ctx, api, args.ChannelID)
		if err != nil {
			return notificationChannelErrorResult(err, map[int]string{
				http.StatusForbidden: "attaching requires a token that can read and write notification channels",
			})
		}
		if !found {
			return utils.ToolErrorResult("channel_id is not a channel id; nothing was sent. Use the id column of get_notification_channels with scope=channels"), nil, nil
		}
		done := make([]notificationChannelAttachment, 0, len(severities))
		for _, sev := range severities {
			row, err := notifications.AttachNotificationSetting(ctx, api, args.ChannelID,
				notifications.AttachNotificationSettingInput{EntityID: groupID, Severity: sev, Property: property})
			if err != nil {
				return attachNotificationChannelFailure(err, sev, done)
			}
			done = append(done, notificationChannelAttachment{AttachmentID: row.ID, AlertGroupID: groupID, Severity: sev})
		}
		return notificationChannelJSONResult(cfg, map[string]any{"attachments": done})
	}
}

func attachNotificationChannelFailure(err error, severity string, done []notificationChannelAttachment) (*mcp.CallToolResult, any, error) {
	res, _, rerr := notificationChannelErrorResult(err, map[int]string{
		http.StatusBadRequest: "the API rejected the attach (HTTP 400); check channel_id (get_notification_channels scope=channels) and alert_group_id (get_alert_groups)",
		http.StatusNotFound:   "channel not found; check channel_id with get_notification_channels scope=channels",
		http.StatusForbidden:  "attaching requires a write-scope token whose owner has the editor or admin role",
	})
	if rerr != nil || len(done) == 0 {
		return res, nil, rerr
	}
	// No rollback: detaching needs a delete-scope token this write may not have. The API
	// upserts attachments per alert group and severity, so either retry finishes the job.
	cause := res.Content[0].(*mcp.TextContent).Text
	return utils.ToolErrorResult(fmt.Sprintf("attached %s (attachment_id %d) but %s failed: %s. %s stays attached; to finish, retry with severity=%s (re-attaching is idempotent, so severity=both is also safe)",
		done[0].Severity, done[0].AttachmentID, severity, cause, done[0].Severity, severity)), nil, nil
}
