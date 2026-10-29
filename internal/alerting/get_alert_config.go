package alerting

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"last9-mcp/internal/deeplink"
	"last9-mcp/internal/last9api"
	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type GetAlertConfigArgs struct {
	RuleID                         string   `json:"rule_id,omitempty" jsonschema:"Exact match on alert rule ID (optional)"`
	SearchTerm                     string   `json:"search_term,omitempty" jsonschema:"Case-insensitive substring search across rule name and alert group metadata (optional)"`
	RuleName                       string   `json:"rule_name,omitempty" jsonschema:"Case-insensitive substring match on rule name (optional)"`
	Severity                       string   `json:"severity,omitempty" jsonschema:"Exact case-insensitive severity filter (optional, e.g. breach or threat)"`
	RuleType                       string   `json:"rule_type,omitempty" jsonschema:"Derived rule type filter (optional, allowed values: static, anomaly, or adaptive)"`
	AlertGroupName                 string   `json:"alert_group_name,omitempty" jsonschema:"Case-insensitive substring match on alert group name (optional)"`
	AlertGroupType                 string   `json:"alert_group_type,omitempty" jsonschema:"Case-insensitive substring match on alert group type (optional)"`
	DataSourceName                 string   `json:"data_source_name,omitempty" jsonschema:"Case-insensitive substring match on alert group data source name (optional)"`
	Tags                           []string `json:"tags,omitempty" jsonschema:"Alert group tag filters combined with AND semantics (optional)"`
	OnlyWithoutNotificationChannel bool     `json:"only_without_notification_channel,omitempty" jsonschema:"If true, include rules whose alert group has no per-entity notification channel binding (dashboard Not configured). OR-combined with notification_channel_types when both are set. Global org-wide channels do not satisfy per-entity filters."`
	NotificationChannelTypes       []string `json:"notification_channel_types,omitempty" jsonschema:"Include rules whose alert group has a per-entity channel binding with any listed type (case-insensitive, e.g. slack, email, pagerduty, generic_webhook). OR-combined with only_without_notification_channel when both are set."`
	NotificationChannelNames       []string `json:"notification_channel_names,omitempty" jsonschema:"Include rules whose alert group has a per-entity channel binding with any listed channel name (case-insensitive exact match). AND-combined with other notification_channel_* filters on the same binding row."`
	NotificationChannelSeverities  []string `json:"notification_channel_severities,omitempty" jsonschema:"Include rules whose alert group has a per-entity channel binding with any listed severity (breach or threat, case-insensitive). AND-combined with notification_channel_types and notification_channel_names on the same binding row. OR-combined with only_without_notification_channel when both are set."`
}

func NewGetAlertConfigHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, GetAlertConfigArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args GetAlertConfigArgs) (*mcp.CallToolResult, any, error) {
		if err := validateGetAlertConfigArgs(args); err != nil {
			return nil, nil, err
		}

		var (
			channelIndex            map[string][]perEntityChannelBinding
			entityChannelsByID      map[string][]NotificationChannel
			globalChannelAdvisory   string
			notificationChannelsErr string
		)

		// See isUnconfiguredOnlyRequest for the semantics. A channel-fetch
		// failure returns early below whenever this is true (the flag implies
		// requiresNotificationChannelJoin), so the advisory is always populated
		// when it is prepended.
		unconfiguredOnly := isUnconfiguredOnlyRequest(args)

		channels, chErr := fetchNotificationChannels(ctx, client, cfg)
		if chErr != nil {
			if requiresNotificationChannelJoin(args) {
				return nil, nil, fmt.Errorf("failed to fetch notification channels: %w", chErr)
			}
			notificationChannelsErr = chErr.Error()
		} else {
			channelIndex = buildEntityNotificationChannelIndex(channels)
			entityChannelsByID = groupPerEntityNotificationChannels(channels)
			globalChannelAdvisory = formatGlobalNotificationChannelAdvisory(channels)
		}

		alertConfig, err := fetchAlertConfig(ctx, client, cfg)
		if err != nil {
			return nil, nil, err
		}

		filteredAlertConfig := filterAlertConfigByRuleFields(alertConfig, args)
		if requiresNotificationChannelJoin(args) && channelIndex != nil {
			filteredAlertConfig = filterAlertRulesByNotificationChannels(
				filteredAlertConfig,
				channelIndex,
				args.NotificationChannelTypes,
				args.NotificationChannelNames,
				args.NotificationChannelSeverities,
				args.OnlyWithoutNotificationChannel,
			)
		}

		entitiesByID := make(map[string]alertGroupEntity)
		if len(filteredAlertConfig) > 0 {
			fetchedEntities, ferr := fetchAlertGroupEntities(ctx, client, cfg, alertGroupEntityQueryFromConfig(args))
			if ferr != nil && requiresAlertGroupEntityLookup(args) {
				return nil, nil, fmt.Errorf("failed to fetch alert group entities: %w", ferr)
			}
			if ferr == nil {
				entitiesByID = fetchedEntities
			}

			filteredAlertConfig = filterAlertConfigByEntityFieldsAndSearch(
				filteredAlertConfig,
				entitiesByID,
				args,
			)
		}

		if !unconfiguredOnly {
			resolveAlertConfigKPIs(ctx, client, cfg, filteredAlertConfig)
		}

		evaluationNote := attachRecentEvaluations(ctx, last9api.NewClient(client, cfg), filteredAlertConfig, time.Now())

		formattedResponse := formatAlertConfigResponse(
			filteredAlertConfig,
			entitiesByID,
			entityChannelsByID,
			notificationChannelsErr,
			unconfiguredOnly,
		)
		if unconfiguredOnly {
			formattedResponse = globalChannelAdvisory + "\n" + formattedResponse
		}
		formattedResponse = evaluationNote + formattedResponse

		// Build deep link URL to alerting groups page
		dlBuilder := deeplink.NewBuilder(cfg.OrgSlug, cfg.ClusterID)
		dashboardURL := dlBuilder.BuildAlertingGroupsLink()

		return &mcp.CallToolResult{
			Meta: deeplink.ToMeta(dashboardURL),
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: formattedResponse,
				},
			},
		}, nil, nil
	}
}
