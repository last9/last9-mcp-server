package alerting

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"last9-mcp/internal/deeplink"
	"last9-mcp/internal/last9api"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type GetAlertConfigArgs struct {
	RuleID                         string   `json:"rule_id,omitempty" jsonschema:"Case-insensitive exact match on alert rule ID (optional; preferred over alert_rule_id)"`
	AlertRuleID                    string   `json:"alert_rule_id,omitempty" jsonschema:"Alias for rule_id (optional; if both are supplied they must identify the same rule)"`
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

		evaluationNote := attachRecentEvaluations(ctx, last9api.NewClient(client, cfg), filteredAlertConfig, utils.Now())

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

func PrepareAlertConfigArguments(raw json.RawMessage) (json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return raw, nil
	}
	if _, ok := fields["alert_id"]; ok {
		return nil, errors.New(`get_alert_config does not take "alert_id": it looks up rules, not alert instances. Pass the triggering rule's rule_id, shown as Rule ID by get_alerts.`)
	}
	alias, ok := fields["alert_rule_id"]
	if !ok {
		return raw, nil
	}
	var id string
	if err := json.Unmarshal(alias, &id); err != nil || strings.TrimSpace(id) == "" {
		return nil, errors.New("alert_rule_id must be a nonempty string")
	}
	if canonical, ok := fields["rule_id"]; ok {
		var current *string
		if err := json.Unmarshal(canonical, &current); err != nil || current == nil || strings.TrimSpace(*current) == "" {
			return nil, errors.New("rule_id must be a nonempty string when alert_rule_id is also supplied")
		}
		// Same equivalence as the rule_id filter, so two spellings of one rule never conflict.
		if !strings.EqualFold(strings.TrimSpace(*current), strings.TrimSpace(id)) {
			return nil, errors.New("conflicting rule_id and alert_rule_id: supply one rule ID")
		}
	}
	fields["rule_id"] = alias
	delete(fields, "alert_rule_id")
	return json.Marshal(fields)
}
