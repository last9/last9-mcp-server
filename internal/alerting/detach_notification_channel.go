package alerting

import (
	"context"
	"net/http"
	"strings"

	"last9-mcp/internal/last9api"
	"last9-mcp/internal/last9api/notifications"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type DetachNotificationChannelArgs struct {
	AttachmentID int    `json:"attachment_id" jsonschema:"(Required) Attachment id: the id column of get_notification_channels with scope=attached (default). Not a channel id"`
	AlertGroupID string `json:"alert_group_id,omitempty" jsonschema:"Alert group id the attachment must belong to; refuses on mismatch. Default: no check"`
}

func NewDetachNotificationChannelHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, DetachNotificationChannelArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args DetachNotificationChannelArgs) (*mcp.CallToolResult, any, error) {
		if args.AttachmentID <= 0 {
			return utils.ToolErrorResult("attachment_id is required; copy it from get_notification_channels (scope=attached)"), nil, nil
		}
		api := last9api.NewClient(client, cfg)
		rows, err := notifications.ListNotificationSettings(ctx, api, true)
		if err != nil {
			return notificationChannelErrorResult(err, map[int]string{
				http.StatusForbidden: "listing attachments requires a read-capable token",
			})
		}
		var row *notifications.NotificationSetting
		for i := range rows {
			if rows[i].ID == args.AttachmentID {
				row = &rows[i]
				break
			}
		}
		if row == nil || strings.TrimSpace(row.ServiceFQID) == "" {
			return utils.ToolErrorResult("attachment_id is not an attachment row. If it is a channel id, detaching would delete the channel, so nothing was sent. Use the id column of get_notification_channels with scope=attached"), nil, nil
		}
		if group := strings.TrimSpace(args.AlertGroupID); group != "" && !strings.EqualFold(group, row.ServiceFQID) {
			return utils.ToolErrorResult("that attachment belongs to a different alert group than alert_group_id; nothing was sent"), nil, nil
		}
		if err := notifications.DetachNotificationSetting(ctx, api, row.ID); err != nil {
			return notificationChannelErrorResult(err, map[int]string{
				http.StatusForbidden: "detaching requires a delete-scope token; a write-scope token cannot detach",
			})
		}
		return notificationChannelJSONResult(cfg, map[string]any{
			"detached": true, "attachment_id": row.ID, "alert_group_id": row.ServiceFQID,
			"channel_name": row.Name, "severity": row.Severity,
		})
	}
}
