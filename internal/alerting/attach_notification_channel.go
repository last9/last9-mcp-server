package alerting

import (
	"context"
	"net/http"
	"strconv"
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
		done := make([]notificationChannelAttachment, 0, len(severities))
		for _, sev := range severities {
			row, err := notifications.AttachNotificationSetting(ctx, api, args.ChannelID,
				notifications.AttachNotificationSettingInput{EntityID: groupID, Severity: sev})
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
	cause := res.Content[0].(*mcp.TextContent).Text
	return utils.ToolErrorResult("attached " + done[0].Severity + " (attachment_id " + strconv.Itoa(done[0].AttachmentID) + ") but " + severity + " failed: " + cause), nil, nil
}
