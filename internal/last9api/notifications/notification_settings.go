package notifications

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"

	"last9-mcp/internal/last9api"
)

const pathNotificationSettings = "/notification_settings/"

// NotificationSetting is a channel or a per-alert-group attachment row. It deliberately
// omits the destination and property fields so secrets can never reach a tool result.
type NotificationSetting struct {
	ID           int    `json:"id"`
	Name         string `json:"name"`
	Type         string `json:"type"`
	SendResolved *bool  `json:"send_resolved"`
	ServiceFQID  string `json:"service_fqid"`
	Severity     string `json:"severity"`
}

type Property struct {
	ServiceOwnerHandle   string            `json:"service_owner_handle,omitempty"`
	WebhookHeaders       map[string]string `json:"webhook_headers,omitempty"`
	WebhookCACertificate string            `json:"webhook_ca_certificate,omitempty"`
	SlackAppMode         bool              `json:"slack_app_mode,omitempty"`
}

type CreateNotificationSettingInput struct {
	Name         string    `json:"name"`
	Type         string    `json:"type"`
	Destination  string    `json:"destination"`
	SendResolved *bool     `json:"send_resolved,omitempty"`
	Property     *Property `json:"property,omitempty"`
}

// ListNotificationSettings calls GET /notification_settings/. exact=true returns only
// per-alert-group attachment rows; otherwise the org's channels.
func ListNotificationSettings(ctx context.Context, c *last9api.Client, exact bool) ([]NotificationSetting, error) {
	var opts []last9api.Option
	if exact {
		opts = append(opts, last9api.WithQuery(url.Values{"exact": {"true"}}))
	}
	var out []NotificationSetting
	err := c.Do(ctx, "list notification settings",
		last9api.Request{Method: http.MethodGet, Path: pathNotificationSettings}, &out, opts...)
	return out, err
}

// CreateNotificationSetting calls POST /notification_settings/. Without an entity it
// creates a channel that can then be attached to alert groups.
func CreateNotificationSetting(ctx context.Context, c *last9api.Client, in CreateNotificationSettingInput) (*NotificationSetting, error) {
	var out NotificationSetting
	err := c.Do(ctx, "create notification channel",
		last9api.Request{Method: http.MethodPost, Path: pathNotificationSettings, Body: in}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// ChannelProperty returns a channel's raw property object, found via GET /notification_settings/.
// found is false when id is not a channel (missing, or a per-alert-group attachment row).
// The property can hold secrets; callers forward it and never surface it.
func ChannelProperty(ctx context.Context, c *last9api.Client, id int) (property json.RawMessage, found bool, err error) {
	var rows []struct {
		ID          int             `json:"id"`
		ServiceFQID string          `json:"service_fqid"`
		Property    json.RawMessage `json:"property"`
	}
	if err := c.Do(ctx, "list notification settings",
		last9api.Request{Method: http.MethodGet, Path: pathNotificationSettings}, &rows); err != nil {
		return nil, false, err
	}
	for _, r := range rows {
		if r.ID == id {
			if r.ServiceFQID != "" {
				return nil, false, nil
			}
			if string(r.Property) == "null" {
				r.Property = nil
			}
			return r.Property, true, nil
		}
	}
	return nil, false, nil
}
