package notifications

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"last9-mcp/internal/last9api"
)

type AttachNotificationSettingInput struct {
	EntityID string          `json:"entity_id"`
	Severity string          `json:"severity"`
	Property json.RawMessage `json:"property,omitempty"`
}

func attachPath(id int) string { return pathNotificationSettings + strconv.Itoa(id) + "/attach" }

// AttachNotificationSetting calls POST /notification_settings/{id}/attach, where id is a channel id.
func AttachNotificationSetting(ctx context.Context, c *last9api.Client, id int, in AttachNotificationSettingInput) (*NotificationSetting, error) {
	var out NotificationSetting
	err := c.Do(ctx, "attach notification channel",
		last9api.Request{Method: http.MethodPost, Path: attachPath(id), Body: in}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// DetachNotificationSetting calls DELETE /notification_settings/{id}/attach, where id is an
// attachment row id. Given a channel id with no attachments the API deletes the channel.
func DetachNotificationSetting(ctx context.Context, c *last9api.Client, id int) error {
	return c.Do(ctx, "detach notification channel",
		last9api.Request{Method: http.MethodDelete, Path: attachPath(id)}, nil)
}

// DeleteNotificationChannel calls DELETE /notification_settings/{id}/attach, where id is a
// channel id. The API deletes the channel only when it has no attachments (otherwise 422).
func DeleteNotificationChannel(ctx context.Context, c *last9api.Client, id int) error {
	return c.Do(ctx, "delete notification channel",
		last9api.Request{Method: http.MethodDelete, Path: attachPath(id)}, nil)
}
