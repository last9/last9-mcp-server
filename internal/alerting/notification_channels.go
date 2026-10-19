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
	"last9-mcp/internal/last9api"
	"last9-mcp/internal/last9api/notifications"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

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
	CreatedVia   string                       `json:"created_via"`
}

// orgWide reports whether the row is unscoped. The API's global flag reflects only the
// legacy service/namespace binding, so it is true on alert-group attachment rows too.
func (ch NotificationChannel) orgWide() bool {
	return ch.Global && strings.TrimSpace(ch.ServiceFQID) == ""
}

type notificationChannelService struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

type GetNotificationChannelsArgs struct {
	Scope string `json:"scope,omitempty" jsonschema:"attached (default) lists per-alert-group attachment rows; channels lists the org's channels (id, name, type, send_resolved), the ids create_notification_channel returns and attach_notification_channel takes"`
}

func NewGetNotificationChannelsHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, GetNotificationChannelsArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args GetNotificationChannelsArgs) (*mcp.CallToolResult, any, error) {
		scope := strings.ToLower(strings.TrimSpace(args.Scope))
		if scope != "" && scope != "attached" && scope != "channels" {
			return utils.ToolErrorResult("scope must be attached or channels"), nil, nil
		}
		dlBuilder := deeplink.NewBuilder(cfg.OrgSlug, cfg.ClusterID)
		dashboardURL := dlBuilder.BuildNotificationChannelsLink()

		if scope == "channels" {
			rows, err := notifications.ListNotificationSettings(ctx, last9api.NewClient(client, cfg), false)
			if err != nil {
				return notificationChannelErrorResult(err, map[int]string{
					http.StatusForbidden: "listing channels requires a token that can read notification channels",
				})
			}
			return &mcp.CallToolResult{
				Meta:    deeplink.ToMeta(dashboardURL),
				Content: []mcp.Content{&mcp.TextContent{Text: formatChannelsScopeResponse(rows)}},
			}, nil, nil
		}

		channels, err := fetchNotificationChannels(ctx, client, cfg)
		if err != nil {
			return nil, nil, err
		}

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

func formatChannelsScopeResponse(rows []notifications.NotificationSetting) string {
	lines := []string{"id\tname\ttype\tsend_resolved\tcreated_via"}
	for _, r := range rows {
		if strings.TrimSpace(r.ServiceFQID) != "" {
			continue
		}
		sendResolved := "null"
		if r.SendResolved != nil {
			sendResolved = fmt.Sprintf("%v", *r.SendResolved)
		}
		lines = append(lines, fmt.Sprintf("%d\t%s\t%s\t%s\t%s", r.ID, escapeTSV(r.Name), escapeTSV(r.Type), sendResolved, escapeTSV(orDash(r.CreatedVia))))
	}
	return fmt.Sprintf("Found %d notification channel(s):\n", len(lines)-1) + strings.Join(lines, "\n")
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
	rows = append(rows, "id\tname\ttype\tglobal\tin_use\tsend_resolved\tsnoozed_until\tseverity\tpriority\tservices\tservice_fqid\tcreated_via")

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

		rows = append(rows, fmt.Sprintf("%d\t%s\t%s\t%v\t%v\t%s\t%s\t%s\t%d\t%s\t%s\t%s",
			ch.ID, escapeTSV(ch.Name), escapeTSV(ch.Type), ch.orgWide(), ch.InUse,
			escapeTSV(sendResolved), escapeTSV(snoozeUntil), escapeTSV(ch.Severity), ch.Priority, escapeTSV(services),
			escapeTSV(serviceFQID), escapeTSV(orDash(ch.CreatedVia)),
		))
	}

	return strings.Join(rows, "\n")
}

func orDash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
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
		hasActiveNotificationChannelFilters(args)
}

// isUnconfiguredOnlyRequest reports whether the request is a pure
// only_without_notification_channel query with no active
// notification_channel_* filters. Such requests report unconfigured rules
// exclusively: the handler uses the unconfigured-only count header, skips KPI
// resolution, and prepends the global-channel advisory. When channel filters
// are OR-combined with only_without_notification_channel, the result set also
// contains configured rules matched via the channel-binding branch, so none of
// those behaviors apply.
func isUnconfiguredOnlyRequest(args GetAlertConfigArgs) bool {
	return args.OnlyWithoutNotificationChannel &&
		!hasActiveNotificationChannelFilters(args)
}

func hasActiveNotificationChannelFilters(args GetAlertConfigArgs) bool {
	return len(normalizeNotificationChannelTypes(args.NotificationChannelTypes)) > 0 ||
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
