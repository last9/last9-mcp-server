package alerting

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"last9-mcp/internal/deeplink"
	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const alertRuleStateDisabled = "disabled"

type GetAlertGroupsArgs struct {
	AlertGroupName string `json:"alert_group_name,omitempty" jsonschema:"Case-insensitive substring match on alert group name (optional)"`
	AlertGroupType string `json:"alert_group_type,omitempty" jsonschema:"Case-insensitive substring match on alert group type (optional)"`
	DataSourceName string `json:"data_source_name,omitempty" jsonschema:"Case-insensitive substring match on alert group data source name (optional)"`
	Team           string `json:"team,omitempty" jsonschema:"Exact case-insensitive match on configured alert group team (optional)"`
	Tier           string `json:"tier,omitempty" jsonschema:"Exact case-insensitive match on configured alert group tier (optional)"`
	LabelKey       string `json:"label_key,omitempty" jsonschema:"Configured metadata label key. Must be set together with label_value (optional)"`
	LabelValue     string `json:"label_value,omitempty" jsonschema:"Configured metadata label value. Must be set together with label_key (optional). Exact case-insensitive match"`
}

type alertGroupsResponse struct {
	Count  int                `json:"count"`
	Groups []alertGroupResult `json:"groups"`
}

type alertGroupResult struct {
	ID                 string                   `json:"id"`
	Name               string                   `json:"name"`
	Type               string                   `json:"type"`
	EntityClass        string                   `json:"entity_class"`
	Team               string                   `json:"team"`
	Tier               string                   `json:"tier"`
	Metadata           alertGroupResultMetadata `json:"metadata"`
	RulesCount         int                      `json:"rules_count"`
	EnabledRulesCount  int                      `json:"enabled_rules_count"`
	DisabledRulesCount int                      `json:"disabled_rules_count"`
}

type alertGroupResultMetadata struct {
	Labels map[string]string `json:"labels"`
}

type alertRuleCounts struct {
	Total    int
	Enabled  int
	Disabled int
}

func NewGetAlertGroupsHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, GetAlertGroupsArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args GetAlertGroupsArgs) (*mcp.CallToolResult, any, error) {
		if err := validateGetAlertGroupsArgs(args); err != nil {
			return nil, nil, err
		}

		query := alertGroupEntityQueryFromGroups(args)
		entitiesByID, err := fetchAlertGroupEntities(ctx, client, cfg, query)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to fetch alert groups: %w", err)
		}

		rules, err := fetchAlertConfig(ctx, client, cfg)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to fetch alert rules: %w", err)
		}

		counts := countAlertRulesByEntity(rules)
		groups := make([]alertGroupResult, 0, len(entitiesByID))
		for _, entity := range entitiesByID {
			if !matchesAlertGroupEntityFilters(entity, true, query) {
				continue
			}
			groups = append(groups, projectAlertGroupResult(entity, counts[entity.ID]))
		}
		sort.Slice(groups, func(i, j int) bool {
			if groups[i].Name != groups[j].Name {
				return groups[i].Name < groups[j].Name
			}
			return groups[i].ID < groups[j].ID
		})

		body, err := json.Marshal(alertGroupsResponse{
			Count:  len(groups),
			Groups: groups,
		})
		if err != nil {
			return nil, nil, fmt.Errorf("failed to encode alert groups: %w", err)
		}

		dlBuilder := deeplink.NewBuilder(cfg.OrgSlug, cfg.ClusterID)
		return &mcp.CallToolResult{
			Meta: deeplink.ToMeta(dlBuilder.BuildAlertingGroupsLink()),
			Content: []mcp.Content{
				&mcp.TextContent{Text: string(body)},
			},
		}, nil, nil
	}
}

func validateGetAlertGroupsArgs(args GetAlertGroupsArgs) error {
	key := strings.TrimSpace(args.LabelKey)
	value := strings.TrimSpace(args.LabelValue)
	if (key == "") != (value == "") {
		return fmt.Errorf("label_key and label_value must be set together")
	}
	return nil
}

func alertGroupEntityQueryFromGroups(args GetAlertGroupsArgs) alertGroupEntityQuery {
	return alertGroupEntityQuery{
		AlertGroupName: args.AlertGroupName,
		AlertGroupType: args.AlertGroupType,
		DataSourceName: args.DataSourceName,
		Team:           args.Team,
		Tier:           args.Tier,
		LabelKey:       args.LabelKey,
		LabelValue:     args.LabelValue,
	}
}

func countAlertRulesByEntity(rules AlertConfigResponse) map[string]alertRuleCounts {
	counts := make(map[string]alertRuleCounts)
	for _, rule := range rules {
		if rule.DeletedAt != nil {
			continue
		}
		current := counts[rule.EntityID]
		current.Total++
		if strings.EqualFold(rule.State, alertRuleStateDisabled) {
			current.Disabled++
		} else {
			current.Enabled++
		}
		counts[rule.EntityID] = current
	}
	return counts
}

func projectAlertGroupResult(entity alertGroupEntity, counts alertRuleCounts) alertGroupResult {
	labels := entity.Metadata.Labels
	if labels == nil {
		labels = map[string]string{}
	}
	return alertGroupResult{
		ID:          entity.ID,
		Name:        entity.Name,
		Type:        entity.Type,
		EntityClass: entity.EntityClass,
		Team:        entity.Metadata.Team,
		Tier:        entity.Tier,
		Metadata: alertGroupResultMetadata{
			Labels: labels,
		},
		RulesCount:         counts.Total,
		EnabledRulesCount:  counts.Enabled,
		DisabledRulesCount: counts.Disabled,
	}
}
