package alerting

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"last9-mcp/internal/constants"
	"last9-mcp/internal/deeplink"
	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const GetNotificationChannelsDescription = `
	Get notification channel configurations from Last9.
	Returns all notification channels configured in the organization as a TSV table.

	Columns: id, name, type, global, in_use, send_resolved, snoozed_until, severity, priority, services
	- send_resolved: true/false/null (null = not explicitly configured)
	- snoozed_until: UTC timestamp if snoozed, else "-"
	- services: comma-separated namespace/name pairs, "-" if global
`

// NotificationChannel represents a notification channel configuration from Last9 API
type NotificationChannel struct {
	ID           int                          `json:"id"`
	Name         string                       `json:"name"`
	Type         string                       `json:"type"`
	Global       bool                         `json:"global"`
	InUse        bool                         `json:"in_use"`
	SnoozeUntil  *int64                       `json:"snooze_until"`
	Priority     int                          `json:"priority"`
	Severity     string                       `json:"severity"`
	SendResolved *bool                        `json:"send_resolved"`
	Services     []notificationChannelService `json:"services"`
}

type notificationChannelService struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

type GetNotificationChannelsArgs struct{}

func NewGetNotificationChannelsHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, GetNotificationChannelsArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args GetNotificationChannelsArgs) (*mcp.CallToolResult, any, error) {
		channels, err := fetchNotificationChannels(ctx, client, cfg)
		if err != nil {
			return nil, nil, err
		}

		dlBuilder := deeplink.NewBuilder(cfg.OrgSlug, cfg.ClusterID)
		dashboardURL := dlBuilder.BuildNotificationChannelsLink()

		return &mcp.CallToolResult{
			Meta: deeplink.ToMeta(dashboardURL),
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: formatNotificationChannelsResponse(channels),
				},
			},
		}, nil, nil
	}
}

func fetchNotificationChannels(ctx context.Context, client *http.Client, cfg models.Config) ([]NotificationChannel, error) {
	url := cfg.APIBaseURL + constants.EndpointNotificationSettings
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set(constants.HeaderAccept, constants.HeaderAcceptJSON)
	httpReq.Header.Set(constants.HeaderXLast9APIToken, constants.BearerPrefix+cfg.TokenManager.GetAccessToken(ctx))

	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var channels []NotificationChannel
	if err := json.Unmarshal(body, &channels); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return channels, nil
}

func formatNotificationChannelsResponse(channels []NotificationChannel) string {
	rows := make([]string, 0, len(channels)+2)
	rows = append(rows, fmt.Sprintf("Found %d notification channel(s):", len(channels)))
	rows = append(rows, "id\tname\ttype\tglobal\tin_use\tsend_resolved\tsnoozed_until\tseverity\tpriority\tservices")

	for _, ch := range channels {
		sendResolved := "null"
		if ch.SendResolved != nil {
			sendResolved = fmt.Sprintf("%v", *ch.SendResolved)
		}

		snoozeUntil := "-"
		if ch.SnoozeUntil != nil && *ch.SnoozeUntil > 0 {
			snoozeUntil = time.Unix(*ch.SnoozeUntil, 0).UTC().Format("2006-01-02 15:04:05 UTC")
		}

		services := "-"
		if len(ch.Services) > 0 {
			parts := make([]string, len(ch.Services))
			for i, svc := range ch.Services {
				if svc.Namespace != "" {
					parts[i] = svc.Namespace + "/" + svc.Name
				} else {
					parts[i] = svc.Name
				}
			}
			services = strings.Join(parts, ",")
		}

		rows = append(rows, fmt.Sprintf("%d\t%s\t%s\t%v\t%v\t%s\t%s\t%s\t%d\t%s",
			ch.ID, ch.Name, ch.Type, ch.Global, ch.InUse,
			sendResolved, snoozeUntil, ch.Severity, ch.Priority, services,
		))
	}

	return strings.Join(rows, "\n")
}
