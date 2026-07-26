package alerting

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"last9-mcp/internal/constants"
	"last9-mcp/internal/deeplink"
	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const maxNotificationChannelsErrorBodyBytes = 4096

var notificationChannelsTSVEscaper = strings.NewReplacer(
	"\t", "\\t",
	"\n", "\\n",
	"\r", "\\r",
)

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
	ServiceFQID  string                       `json:"service_fqid"`
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
	// Dashboard Alert Studio loads channels with exact=true, which returns per-entity
	// mapped rows (valid service_fqid). Without exact, the API returns master/global
	// channels only — binding filters would falsely report every rule as unconfigured.
	fullURL := cfg.APIBaseURL + constants.EndpointNotificationSettings + "?exact=true"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if cfg.TokenManager == nil {
		return nil, fmt.Errorf("token manager is not configured")
	}
	token := strings.TrimSpace(cfg.TokenManager.GetAccessToken(ctx))
	if token == "" {
		return nil, fmt.Errorf("access token cannot be empty")
	}

	httpReq.Header.Set(constants.HeaderAccept, constants.HeaderAcceptJSON)
	httpReq.Header.Set(constants.HeaderXLast9APIToken, constants.BearerPrefix+token)

	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxNotificationChannelsErrorBodyBytes+1))
		bodyText := string(body)
		if len(body) > maxNotificationChannelsErrorBodyBytes {
			bodyText = string(body[:maxNotificationChannelsErrorBodyBytes]) + "...(truncated)"
		}
		return nil, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, bodyText)
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
	rows = append(rows, "id\tname\ttype\tglobal\tin_use\tsend_resolved\tsnoozed_until\tseverity\tpriority\tservices\tservice_fqid")

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
				serviceName := escapeTSV(svc.Name)
				serviceNamespace := escapeTSV(svc.Namespace)
				if svc.Namespace != "" {
					parts[i] = serviceNamespace + "/" + serviceName
				} else {
					parts[i] = serviceName
				}
			}
			services = strings.Join(parts, ",")
		}

		serviceFQID := ch.ServiceFQID
		if serviceFQID == "" {
			serviceFQID = "-"
		}

		rows = append(rows, fmt.Sprintf("%d\t%s\t%s\t%v\t%v\t%s\t%s\t%s\t%d\t%s\t%s",
			ch.ID, escapeTSV(ch.Name), escapeTSV(ch.Type), ch.Global, ch.InUse,
			escapeTSV(sendResolved), escapeTSV(snoozeUntil), escapeTSV(ch.Severity), ch.Priority, escapeTSV(services),
			escapeTSV(serviceFQID),
		))
	}

	return strings.Join(rows, "\n")
}

func escapeTSV(value string) string {
	return notificationChannelsTSVEscaper.Replace(value)
}

type entityNotificationChannels struct {
	types map[string]struct{}
	names map[string]struct{}
}

func buildEntityNotificationChannelIndex(channels []NotificationChannel) map[string]entityNotificationChannels {
	index := make(map[string]entityNotificationChannels)
	for _, ch := range channels {
		entityID := strings.TrimSpace(ch.ServiceFQID)
		if entityID == "" {
			continue
		}
		entry := index[entityID]
		if entry.types == nil {
			entry.types = make(map[string]struct{})
			entry.names = make(map[string]struct{})
		}
		channelType := strings.ToLower(strings.TrimSpace(ch.Type))
		if channelType != "" {
			entry.types[channelType] = struct{}{}
		}
		channelName := strings.TrimSpace(ch.Name)
		if channelName != "" {
			entry.names[channelName] = struct{}{}
		}
		index[entityID] = entry
	}
	return index
}

func entityHasPerEntityNotificationChannel(entityID string, index map[string]entityNotificationChannels) bool {
	entry, ok := index[entityID]
	return ok && len(entry.types) > 0
}

func matchesNotificationChannelTypes(entityID string, index map[string]entityNotificationChannels, types []string) bool {
	entry, ok := index[entityID]
	if !ok || len(entry.types) == 0 {
		return false
	}
	for _, wantType := range types {
		wantType = strings.ToLower(strings.TrimSpace(wantType))
		if wantType == "" {
			continue
		}
		if _, ok := entry.types[wantType]; ok {
			return true
		}
	}
	return false
}

func matchesNotificationChannelNames(entityID string, index map[string]entityNotificationChannels, names []string) bool {
	entry, ok := index[entityID]
	if !ok || len(entry.names) == 0 {
		return false
	}
	for _, wantName := range names {
		wantName = strings.TrimSpace(wantName)
		if wantName == "" {
			continue
		}
		for haveName := range entry.names {
			if strings.EqualFold(haveName, wantName) {
				return true
			}
		}
	}
	return false
}

func filterAlertRulesByNotificationChannels(
	rules AlertConfigResponse,
	channelIndex map[string]entityNotificationChannels,
	channelTypes []string,
	channelNames []string,
	onlyWithout bool,
) AlertConfigResponse {
	normalizedTypes := normalizeNotificationChannelTypes(channelTypes)
	normalizedNames := normalizeStringSlice(channelNames)
	if !onlyWithout && len(normalizedTypes) == 0 && len(normalizedNames) == 0 {
		return rules
	}

	filtered := make(AlertConfigResponse, 0, len(rules))
	for _, rule := range rules {
		entityID := rule.EntityID
		hasBindings := entityHasPerEntityNotificationChannel(entityID, channelIndex)

		matchesWithout := onlyWithout && !hasBindings
		matchesType := len(normalizedTypes) > 0 && matchesNotificationChannelTypes(entityID, channelIndex, normalizedTypes)
		matchesName := len(normalizedNames) > 0 && matchesNotificationChannelNames(entityID, channelIndex, normalizedNames)

		channelTypeClause := len(normalizedTypes) == 0 && !onlyWithout
		if len(normalizedTypes) > 0 || onlyWithout {
			channelTypeClause = matchesWithout || matchesType
		}

		if !channelTypeClause {
			continue
		}
		if len(normalizedNames) > 0 && !matchesName {
			continue
		}

		filtered = append(filtered, rule)
	}
	return filtered
}

func normalizeNotificationChannelTypes(types []string) []string {
	normalized := normalizeStringSlice(types)
	out := make([]string, 0, len(normalized))
	for _, t := range normalized {
		out = append(out, strings.ToLower(t))
	}
	return out
}

func requiresNotificationChannelJoin(args GetAlertConfigArgs) bool {
	return args.OnlyWithoutNotificationChannel ||
		len(normalizeNotificationChannelTypes(args.NotificationChannelTypes)) > 0 ||
		len(normalizeStringSlice(args.NotificationChannelNames)) > 0
}

func formatGlobalNotificationChannelAdvisory(channels []NotificationChannel) string {
	names := make([]string, 0)
	for _, ch := range channels {
		if ch.Global && strings.TrimSpace(ch.Name) != "" {
			names = append(names, ch.Name)
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		return "Global notification channels: none."
	}
	return fmt.Sprintf(
		"Global notification channels: %d org-wide channel(s) configured (%s). These do not count as per-alert-group binding (dashboard \"Not configured\" semantics).",
		len(names),
		strings.Join(names, ", "),
	)
}
