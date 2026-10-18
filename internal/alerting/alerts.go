package alerting

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"last9-mcp/internal/deeplink"
	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// AlertRuleExpressionArg holds indicator binding for an alert rule.
// PromQL, Unit, and LookupError are resolved at query time and not part of the API response.
type AlertRuleExpressionArg struct {
	ID          string            `json:"id"`
	Variables   map[string]string `json:"variables"`
	PromQL      string            `json:"-"`
	Unit        string            `json:"-"`
	LookupError string            `json:"-"`
}

// AlertRule represents an alert configuration from Last9 API
type AlertRule struct {
	ID                           string                            `json:"id"`
	OrganizationID               string                            `json:"organization_id"`
	EntityID                     string                            `json:"entity_id"`
	PrimaryIndicator             string                            `json:"primary_indicator"`
	Expression                   string                            `json:"expression,omitempty"`
	Condition                    string                            `json:"condition,omitempty"`
	AlertCondition               string                            `json:"alert_condition,omitempty"`
	EvalWindow                   int64                             `json:"eval_window,omitempty"`
	ExpressionArgs               map[string]AlertRuleExpressionArg `json:"expression_args,omitempty"`
	CreatedAt                    int64                             `json:"created_at"`
	UpdatedAt                    int64                             `json:"updated_at"`
	DeletedAt                    *int64                            `json:"deleted_at"`
	ErrorSince                   *int64                            `json:"error_since"`
	State                        string                            `json:"state"`
	ExternalRef                  string                            `json:"external_ref"`
	Severity                     string                            `json:"severity"`
	Algorithm                    string                            `json:"algorithm"`
	RuleName                     string                            `json:"rule_name"`
	MuteUntil                    int64                             `json:"mute_until"`
	Properties                   map[string]interface{}            `json:"properties"`
	GroupTimeseriesNotifications bool                              `json:"group_timeseries_notifications"`
	RecurringMuteSchedule        json.RawMessage                   `json:"recurring_mute_schedule,omitempty"`
}

// AlertConfigResponse represents the response from alert rules API (direct array)
type AlertConfigResponse []AlertRule

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
