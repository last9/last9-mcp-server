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

type DeleteNotificationChannelArgs struct {
	ChannelID int `json:"channel_id" jsonschema:"(Required) Channel id: the id column of get_notification_channels with scope=channels. Not an attachment id"`
}

func findNotificationSetting(rows []notifications.NotificationSetting, id int) *notifications.NotificationSetting {
	for i := range rows {
		if rows[i].ID == id {
			return &rows[i]
		}
	}
	return nil
}

func attachmentIDResult(row *notifications.NotificationSetting) *mcp.CallToolResult {
	return utils.ToolErrorResult(fmt.Sprintf("channel_id %d is an attachment id (alert group %s, severity %s), not a channel; nothing was deleted. Remove the attachment with detach_notification_channel, or take a channel id from get_notification_channels with scope=channels", row.ID, row.ServiceFQID, row.Severity))
}

func NewDeleteNotificationChannelHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, DeleteNotificationChannelArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args DeleteNotificationChannelArgs) (*mcp.CallToolResult, any, error) {
		if args.ChannelID <= 0 {
			return utils.ToolErrorResult("channel_id is required; copy it from get_notification_channels (scope=channels)"), nil, nil
		}
		api := last9api.NewClient(client, cfg)
		listHints := map[int]string{http.StatusForbidden: "listing channels requires a read-capable token"}
		rows, err := notifications.ListNotificationSettings(ctx, api, false)
		if err != nil {
			return notificationChannelErrorResult(err, listHints)
		}
		row := findNotificationSetting(rows, args.ChannelID)
		if row == nil {
			// The channel list omits attachment rows, so look there before calling the id unknown.
			attachments, err := notifications.ListNotificationSettings(ctx, api, true)
			if err != nil {
				return notificationChannelErrorResult(err, listHints)
			}
			if attachment := findNotificationSetting(attachments, args.ChannelID); attachment != nil {
				return attachmentIDResult(attachment), nil, nil
			}
			return utils.ToolErrorResult("no channel or attachment with that id; nothing was deleted. Copy channel_id from get_notification_channels with scope=channels"), nil, nil
		}
		if strings.TrimSpace(row.ServiceFQID) != "" {
			return attachmentIDResult(row), nil, nil
		}
		// Attachments link to a channel by its secret destination, so the API's 422 is the guard.
		if err := notifications.DeleteNotificationChannel(ctx, api, row.ID); err != nil {
			return notificationChannelErrorResult(err, map[int]string{
				http.StatusForbidden:           "deleting a channel requires a delete-scope token; a write-scope token cannot delete",
				http.StatusUnprocessableEntity: "channel is attached to alert group(s); detach those attachments first with detach_notification_channel (find them with get_notification_channels scope=attached)",
			})
		}
		after, err := notifications.ListNotificationSettings(ctx, api, false)
		if err != nil {
			return notificationChannelErrorResult(err, listHints)
		}
		if findNotificationSetting(after, row.ID) != nil {
			return utils.ToolErrorResult("delete was not applied: the channel still exists"), nil, nil
		}
		return notificationChannelJSONResult(cfg, map[string]any{
			"deleted": true, "id": row.ID, "name": row.Name, "type": row.Type,
		})
	}
}
