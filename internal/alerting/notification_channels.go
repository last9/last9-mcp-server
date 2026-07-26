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

type perEntityChannelBinding struct {
	channelType string
	name        string
	severity    string
}

func groupPerEntityNotificationChannels(channels []NotificationChannel) map[string][]NotificationChannel {
	grouped := make(map[string][]NotificationChannel)
	for _, ch := range channels {
		entityID := strings.TrimSpace(ch.ServiceFQID)
		if entityID == "" {
			continue
		}
		grouped[entityID] = append(grouped[entityID], ch)
	}
	return grouped
}

// dashboardChannelTypeOrder matches Alert Studio rules table icon order.
var dashboardChannelTypeOrder = []string{
	"opsgenie",
	"pagerduty",
	"slack",
	"generic_webhook",
	"email",
}

func formatNotificationChannelSummary(bindings []NotificationChannel) string {
	if len(bindings) == 0 {
		return "Not configured"
	}

	typesPresent := make(map[string]struct{})
	for _, ch := range bindings {
		channelType := strings.ToLower(strings.TrimSpace(ch.Type))
		if channelType != "" {
			typesPresent[channelType] = struct{}{}
		}
	}
	if len(typesPresent) == 0 {
		return "Not configured"
	}

	orderedTypes := make([]string, 0, len(typesPresent))
	for _, channelType := range dashboardChannelTypeOrder {
		if _, ok := typesPresent[channelType]; ok {
			orderedTypes = append(orderedTypes, channelType)
			delete(typesPresent, channelType)
		}
	}
	remaining := make([]string, 0, len(typesPresent))
	for channelType := range typesPresent {
		remaining = append(remaining, channelType)
	}
	sort.Strings(remaining)
	orderedTypes = append(orderedTypes, remaining...)

	return strings.Join(orderedTypes, ", ")
}

func formatNotificationChannelBindingDetails(bindings []NotificationChannel) string {
	if len(bindings) == 0 {
		return ""
	}

	lines := make([]string, 0, len(bindings))
	// Dashboard tooltip order: threat before breach per channel type icon.
	severityOrder := []string{"threat", "breach"}
	for _, channelType := range orderedChannelTypesFromBindings(bindings) {
		channelsOfType := filterBindingsByType(bindings, channelType)
		for _, severity := range severityOrder {
			for _, ch := range filterBindingsBySeverity(channelsOfType, severity) {
				lines = append(lines, formatNotificationChannelBindingLine(ch))
			}
		}
	}

	if len(lines) == 0 {
		return ""
	}
	return "  Notification Channel Bindings:\n" + strings.Join(lines, "\n") + "\n"
}

func orderedChannelTypesFromBindings(bindings []NotificationChannel) []string {
	typesPresent := make(map[string]struct{})
	for _, ch := range bindings {
		channelType := strings.ToLower(strings.TrimSpace(ch.Type))
		if channelType != "" {
			typesPresent[channelType] = struct{}{}
		}
	}

	ordered := make([]string, 0, len(typesPresent))
	for _, channelType := range dashboardChannelTypeOrder {
		if _, ok := typesPresent[channelType]; ok {
			ordered = append(ordered, channelType)
			delete(typesPresent, channelType)
		}
	}
	remaining := make([]string, 0, len(typesPresent))
	for channelType := range typesPresent {
		remaining = append(remaining, channelType)
	}
	sort.Strings(remaining)
	return append(ordered, remaining...)
}

func filterBindingsByType(bindings []NotificationChannel, channelType string) []NotificationChannel {
	out := make([]NotificationChannel, 0)
	for _, ch := range bindings {
		if strings.EqualFold(strings.TrimSpace(ch.Type), channelType) {
			out = append(out, ch)
		}
	}
	return out
}

func filterBindingsBySeverity(bindings []NotificationChannel, severity string) []NotificationChannel {
	out := make([]NotificationChannel, 0)
	for _, ch := range bindings {
		if strings.EqualFold(strings.TrimSpace(ch.Severity), severity) {
			out = append(out, ch)
		}
	}
	return out
}

func formatNotificationChannelBindingLine(ch NotificationChannel) string {
	name := strings.TrimSpace(ch.Name)
	if name == "" {
		name = "(unnamed)"
	}
	channelType := strings.TrimSpace(ch.Type)
	severity := strings.TrimSpace(ch.Severity)
	suffix := ""
	if ch.SnoozeUntil != nil && *ch.SnoozeUntil > 0 {
		suffix = fmt.Sprintf(
			" [snoozed until %s]",
			time.Unix(*ch.SnoozeUntil, 0).UTC().Format("2006-01-02 15:04:05 UTC"),
		)
	}
	if !ch.InUse {
		suffix += " [not in use]"
	}
	return fmt.Sprintf(
		"    %s / %s (%s)%s",
		channelType,
		name,
		severity,
		suffix,
	)
}

func buildEntityNotificationChannelIndex(channels []NotificationChannel) map[string][]perEntityChannelBinding {
	index := make(map[string][]perEntityChannelBinding)
	for _, ch := range channels {
		entityID := strings.TrimSpace(ch.ServiceFQID)
		if entityID == "" {
			continue
		}
		index[entityID] = append(index[entityID], perEntityChannelBinding{
			channelType: strings.ToLower(strings.TrimSpace(ch.Type)),
			name:        strings.TrimSpace(ch.Name),
			severity:    strings.ToLower(strings.TrimSpace(ch.Severity)),
		})
	}
	return index
}

func entityHasPerEntityBindings(entityID string, index map[string][]perEntityChannelBinding) bool {
	return len(index[entityID]) > 0
}

func entityMatchesChannelFilters(
	entityID string,
	index map[string][]perEntityChannelBinding,
	channelTypes []string,
	channelNames []string,
	channelSeverities []string,
) bool {
	bindings := index[entityID]
	if len(bindings) == 0 {
		return false
	}

	wantTypes := normalizeNotificationChannelTypes(channelTypes)
	wantNames := normalizeStringSlice(channelNames)
	wantSeverities := normalizeNotificationChannelSeverities(channelSeverities)

	for _, binding := range bindings {
		if len(wantTypes) > 0 && !containsString(wantTypes, binding.channelType) {
			continue
		}
		if len(wantNames) > 0 && !anyStringEqualFold(wantNames, binding.name) {
			continue
		}
		if len(wantSeverities) > 0 && !containsString(wantSeverities, binding.severity) {
			continue
		}
		return true
	}
	return false
}

func containsString(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}

func anyStringEqualFold(want []string, have string) bool {
	for _, w := range want {
		if strings.EqualFold(w, have) {
			return true
		}
	}
	return false
}

func filterAlertRulesByNotificationChannels(
	rules AlertConfigResponse,
	channelIndex map[string][]perEntityChannelBinding,
	channelTypes []string,
	channelNames []string,
	channelSeverities []string,
	onlyWithout bool,
) AlertConfigResponse {
	normalizedTypes := normalizeNotificationChannelTypes(channelTypes)
	normalizedNames := normalizeStringSlice(channelNames)
	normalizedSeverities := normalizeNotificationChannelSeverities(channelSeverities)
	hasChannelFilter := len(normalizedTypes) > 0 || len(normalizedNames) > 0 || len(normalizedSeverities) > 0
	if !onlyWithout && !hasChannelFilter {
		return rules
	}

	filtered := make(AlertConfigResponse, 0, len(rules))
	for _, rule := range rules {
		entityID := rule.EntityID
		hasBindings := entityHasPerEntityBindings(entityID, channelIndex)

		matchesWithout := onlyWithout && !hasBindings
		matchesChannelFilter := hasChannelFilter &&
			entityMatchesChannelFilters(entityID, channelIndex, channelTypes, channelNames, channelSeverities)

		if !matchesWithout && !matchesChannelFilter {
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

func normalizeNotificationChannelSeverities(severities []string) []string {
	normalized := normalizeStringSlice(severities)
	out := make([]string, 0, len(normalized))
	for _, s := range normalized {
		out = append(out, strings.ToLower(s))
	}
	return out
}

func requiresNotificationChannelJoin(args GetAlertConfigArgs) bool {
	return args.OnlyWithoutNotificationChannel ||
		len(normalizeNotificationChannelTypes(args.NotificationChannelTypes)) > 0 ||
		len(normalizeStringSlice(args.NotificationChannelNames)) > 0 ||
		len(normalizeNotificationChannelSeverities(args.NotificationChannelSeverities)) > 0
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
