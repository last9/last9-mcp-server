package alerting

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"last9-mcp/internal/constants"
	"last9-mcp/internal/models"
)

const (
	alertGroupEntityClassGrafanaAlerts = "grafana-alerts"
	alertGroupEntityClassAlertManager  = "alert-manager"

	alertConfigRuleTypeStatic  = "static"
	alertConfigRuleTypeAnomaly = "anomaly"

	entityFilterContains    = "contains"
	entityFilterEqual       = "equal"
	entityFilterEntityClass = "entity_class"
	entityFilterEntityName  = "entity_name"
	entityFilterEntityType  = "entity_type"
	entityFilterDataSource  = "data_source_name"
	entityFilterTags        = "tags"
	entityFilterTeam        = "team"
	entityFilterTier        = "tier"
	entityFilterLabel       = "label"
)

type alertGroupEntity struct {
	ID             string                   `json:"id"`
	Name           string                   `json:"name"`
	Type           string                   `json:"type"`
	EntityClass    string                   `json:"entity_class"`
	Tier           string                   `json:"tier"`
	DataSourceName string                   `json:"data_source_name"`
	Metadata       alertGroupEntityMetadata `json:"metadata"`
}

type alertGroupEntityMetadata struct {
	Tags   []string          `json:"tags"`
	Team   string            `json:"team"`
	Labels map[string]string `json:"labels"`
}

// alertGroupEntityQuery is the Compass /entities/list filter shared by
// get_alert_config (name/type/datasource/tags) and get_alert_groups
// (those plus team/tier/label).
type alertGroupEntityQuery struct {
	AlertGroupName string
	AlertGroupType string
	DataSourceName string
	Tags           []string
	Team           string
	Tier           string
	LabelKey       string
	LabelValue     string
}

func alertGroupEntityQueryFromConfig(args GetAlertConfigArgs) alertGroupEntityQuery {
	return alertGroupEntityQuery{
		AlertGroupName: args.AlertGroupName,
		AlertGroupType: args.AlertGroupType,
		DataSourceName: args.DataSourceName,
		Tags:           args.Tags,
	}
}

func (q alertGroupEntityQuery) hasTypedFilters() bool {
	return strings.TrimSpace(q.AlertGroupName) != "" ||
		strings.TrimSpace(q.AlertGroupType) != "" ||
		strings.TrimSpace(q.DataSourceName) != "" ||
		len(normalizeStringSlice(q.Tags)) > 0 ||
		strings.TrimSpace(q.Team) != "" ||
		strings.TrimSpace(q.Tier) != "" ||
		strings.TrimSpace(q.LabelKey) != "" ||
		strings.TrimSpace(q.LabelValue) != ""
}

type groupedAlertGroupEntitiesResponse struct {
	Entities []alertGroupEntity `json:"entities"`
}

type alertGroupEntityFilter struct {
	FilterType  string `json:"filter_type"`
	FilterKey   string `json:"key"`
	FilterValue string `json:"value"`
	Operator    string `json:"operator"`
	Conjunction string `json:"conjunction,omitempty"`
}

type filterAlertGroupEntitiesRequest struct {
	Filters []alertGroupEntityFilter `json:"filters"`
	Groups  []any                    `json:"groups"`
	Orders  []any                    `json:"orders"`
}

func validateGetAlertConfigArgs(args GetAlertConfigArgs) error {
	for _, severity := range normalizeNotificationChannelSeverities(args.NotificationChannelSeverities) {
		if severity != "breach" && severity != "threat" {
			return fmt.Errorf("notification_channel_severities entries must be %q or %q", "breach", "threat")
		}
	}

	ruleType := strings.ToLower(strings.TrimSpace(args.RuleType))
	if ruleType == "" {
		return nil
	}

	if ruleType != alertConfigRuleTypeStatic && ruleType != alertConfigRuleTypeAnomaly {
		return fmt.Errorf("rule_type must be one of %q or %q", alertConfigRuleTypeStatic, alertConfigRuleTypeAnomaly)
	}

	return nil
}

type kpiDefinition struct {
	Query string `json:"query"`
	Unit  string `json:"unit"`
}

type kpiResponse struct {
	ID         string        `json:"id"`
	Name       string        `json:"name"`
	Definition kpiDefinition `json:"definition"`
}

func fetchKPI(
	ctx context.Context,
	client *http.Client,
	cfg models.Config,
	entityID, kpiID string,
) (kpiResponse, error) {
	fullURL := cfg.APIBaseURL + fmt.Sprintf(constants.EndpointEntityKPI, entityID, kpiID)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
	if err != nil {
		return kpiResponse{}, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set(constants.HeaderAccept, constants.HeaderAcceptJSON)
	httpReq.Header.Set(constants.HeaderXLast9APIToken, constants.BearerPrefix+cfg.TokenManager.GetAccessToken(ctx))

	resp, err := client.Do(httpReq)
	if err != nil {
		return kpiResponse{}, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return kpiResponse{}, fmt.Errorf("API returned %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return kpiResponse{}, fmt.Errorf("failed to read response: %w", err)
	}

	var kpi kpiResponse
	if err := json.Unmarshal(body, &kpi); err != nil {
		return kpiResponse{}, fmt.Errorf("failed to parse response: %w", err)
	}

	return kpi, nil
}

func resolveAlertConfigKPIs(
	ctx context.Context,
	client *http.Client,
	cfg models.Config,
	alertConfig AlertConfigResponse,
) {
	// Cache KPI results by "entityID/kpiID" to avoid redundant fetches when
	// multiple rules share the same KPI (common across entity-scoped rules).
	type kpiCacheKey struct{ entityID, kpiID string }
	cache := make(map[kpiCacheKey]kpiResponse)
	errCache := make(map[kpiCacheKey]string)

	for i := range alertConfig {
		rule := &alertConfig[i]
		for indicatorName, arg := range rule.ExpressionArgs {
			if ctx.Err() != nil {
				arg.LookupError = "context cancelled"
				rule.ExpressionArgs[indicatorName] = arg
				continue
			}
			key := kpiCacheKey{rule.EntityID, arg.ID}
			if errMsg, ok := errCache[key]; ok {
				arg.LookupError = errMsg
			} else if kpi, ok := cache[key]; ok {
				arg.PromQL = kpi.Definition.Query
				arg.Unit = kpi.Definition.Unit
			} else {
				kpi, err := fetchKPI(ctx, client, cfg, rule.EntityID, arg.ID)
				if err != nil {
					errCache[key] = err.Error()
					arg.LookupError = err.Error()
				} else {
					cache[key] = kpi
					arg.PromQL = kpi.Definition.Query
					arg.Unit = kpi.Definition.Unit
				}
			}
			rule.ExpressionArgs[indicatorName] = arg
		}
	}
}

func fetchAlertConfig(
	ctx context.Context,
	client *http.Client,
	cfg models.Config,
) (AlertConfigResponse, error) {
	baseURL := fmt.Sprintf("%s%s", cfg.APIBaseURL, constants.EndpointAlertRules)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL, nil)
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

	var alertConfig AlertConfigResponse
	if err := json.Unmarshal(body, &alertConfig); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return alertConfig, nil
}

func fetchAlertGroupEntities(
	ctx context.Context,
	client *http.Client,
	cfg models.Config,
	query alertGroupEntityQuery,
) (map[string]alertGroupEntity, error) {
	requestBody := filterAlertGroupEntitiesRequest{
		Filters: buildAlertGroupEntityLookupFilters(query),
		Groups:  []any{},
		Orders:  []any{},
	}

	bodyBytes, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to encode entity filters: %w", err)
	}

	fullURL := fmt.Sprintf("%s%s", cfg.APIBaseURL, constants.EndpointEntitiesList)
	httpReq, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		fullURL,
		bytes.NewBuffer(bodyBytes),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set(constants.HeaderAccept, constants.HeaderAcceptJSON)
	httpReq.Header.Set(constants.HeaderContentType, constants.HeaderContentTypeJSON)
	httpReq.Header.Set(constants.HeaderXLast9APIToken, constants.BearerPrefix+cfg.TokenManager.GetAccessToken(ctx))

	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("entity lookup failed with status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var groups []groupedAlertGroupEntitiesResponse
	if err := json.Unmarshal(body, &groups); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	entitiesByID := make(map[string]alertGroupEntity)
	for _, group := range groups {
		for _, entity := range group.Entities {
			entitiesByID[entity.ID] = entity
		}
	}

	return entitiesByID, nil
}

func buildAlertGroupEntityLookupFilters(query alertGroupEntityQuery) []alertGroupEntityFilter {
	explicitFilters := make([]alertGroupEntityFilter, 0, 4+len(normalizeStringSlice(query.Tags)))

	if alertGroupName := strings.TrimSpace(query.AlertGroupName); alertGroupName != "" {
		explicitFilters = append(explicitFilters, newAlertGroupEntityFilter(
			entityFilterEntityName,
			alertGroupName,
			entityFilterContains,
		))
	}

	if alertGroupType := strings.TrimSpace(query.AlertGroupType); alertGroupType != "" {
		explicitFilters = append(explicitFilters, newAlertGroupEntityFilter(
			entityFilterEntityType,
			alertGroupType,
			entityFilterContains,
		))
	}

	if dataSourceName := strings.TrimSpace(query.DataSourceName); dataSourceName != "" {
		explicitFilters = append(explicitFilters, newAlertGroupEntityFilter(
			entityFilterDataSource,
			dataSourceName,
			entityFilterContains,
		))
	}

	for _, tag := range normalizeStringSlice(query.Tags) {
		explicitFilters = append(explicitFilters, newAlertGroupEntityFilter(
			entityFilterTags,
			tag,
			entityFilterContains,
		))
	}

	if team := strings.TrimSpace(query.Team); team != "" {
		explicitFilters = append(explicitFilters, newAlertGroupEntityFilter(
			entityFilterTeam,
			team,
			entityFilterEqual,
		))
	}

	if tier := strings.TrimSpace(query.Tier); tier != "" {
		explicitFilters = append(explicitFilters, newAlertGroupEntityFilter(
			entityFilterTier,
			tier,
			entityFilterEqual,
		))
	}

	if labelKey := strings.TrimSpace(query.LabelKey); labelKey != "" {
		explicitFilters = append(explicitFilters, newAlertGroupLabelFilter(
			labelKey,
			strings.TrimSpace(query.LabelValue),
		))
	}

	return scopeAlertGroupEntityFiltersToSupportedClasses(explicitFilters)
}

func newAlertGroupEntityFilter(filterType, value, operator string) alertGroupEntityFilter {
	return alertGroupEntityFilter{
		FilterType:  filterType,
		FilterKey:   value,
		FilterValue: value,
		Operator:    operator,
	}
}

func newAlertGroupLabelFilter(key, value string) alertGroupEntityFilter {
	return alertGroupEntityFilter{
		FilterType:  entityFilterLabel,
		FilterKey:   key,
		FilterValue: value,
		Operator:    entityFilterEqual,
	}
}

func scopeAlertGroupEntityFiltersToSupportedClasses(
	explicitFilters []alertGroupEntityFilter,
) []alertGroupEntityFilter {
	classes := []string{
		alertGroupEntityClassGrafanaAlerts,
		alertGroupEntityClassAlertManager,
	}

	filters := make([]alertGroupEntityFilter, 0, len(classes)*(len(explicitFilters)+1))
	for i, class := range classes {
		classFilter := newAlertGroupEntityFilter(entityFilterEntityClass, class, entityFilterEqual)
		if i > 0 {
			classFilter.Conjunction = "or"
		}

		filters = append(filters, classFilter)
		for _, filter := range explicitFilters {
			filters = append(filters, filter)
		}
	}

	return filters
}

func filterAlertConfigByRuleFields(
	alertConfig AlertConfigResponse,
	args GetAlertConfigArgs,
) AlertConfigResponse {
	filtered := make(AlertConfigResponse, 0, len(alertConfig))
	for _, rule := range alertConfig {
		if ruleID := strings.TrimSpace(args.RuleID); ruleID != "" && !strings.EqualFold(rule.ID, ruleID) {
			continue
		}

		if ruleName := strings.TrimSpace(args.RuleName); ruleName != "" && !containsFold(rule.RuleName, ruleName) {
			continue
		}

		if severity := strings.TrimSpace(args.Severity); severity != "" && !strings.EqualFold(rule.Severity, severity) {
			continue
		}

		if ruleType := strings.TrimSpace(args.RuleType); ruleType != "" && !strings.EqualFold(alertConfigRuleType(rule), ruleType) {
			continue
		}

		filtered = append(filtered, rule)
	}

	return filtered
}

func filterAlertConfigByEntityFieldsAndSearch(
	alertConfig AlertConfigResponse,
	entitiesByID map[string]alertGroupEntity,
	args GetAlertConfigArgs,
) AlertConfigResponse {
	if !requiresAlertGroupEntityLookup(args) {
		return alertConfig
	}

	searchTerm := strings.TrimSpace(args.SearchTerm)
	filtered := make(AlertConfigResponse, 0, len(alertConfig))
	for _, rule := range alertConfig {
		entity, entityFound := entitiesByID[rule.EntityID]

		if !matchesAlertGroupEntityFilters(entity, entityFound, alertGroupEntityQueryFromConfig(args)) {
			continue
		}

		if searchTerm != "" && !matchesAlertConfigSearchTerm(rule, entity, entityFound, searchTerm) {
			continue
		}

		filtered = append(filtered, rule)
	}

	return filtered
}

func requiresAlertGroupEntityLookup(args GetAlertConfigArgs) bool {
	return strings.TrimSpace(args.SearchTerm) != "" ||
		alertGroupEntityQueryFromConfig(args).hasTypedFilters()
}

func matchesAlertGroupEntityFilters(
	entity alertGroupEntity,
	entityFound bool,
	query alertGroupEntityQuery,
) bool {
	if !query.hasTypedFilters() {
		return true
	}

	if !entityFound {
		return false
	}

	if alertGroupName := strings.TrimSpace(query.AlertGroupName); alertGroupName != "" && !containsFold(entity.Name, alertGroupName) {
		return false
	}

	if alertGroupType := strings.TrimSpace(query.AlertGroupType); alertGroupType != "" && !containsFold(entity.Type, alertGroupType) {
		return false
	}

	if dataSourceName := strings.TrimSpace(query.DataSourceName); dataSourceName != "" && !containsFold(entity.DataSourceName, dataSourceName) {
		return false
	}

	if team := strings.TrimSpace(query.Team); team != "" && !strings.EqualFold(entity.Metadata.Team, team) {
		return false
	}

	if tier := strings.TrimSpace(query.Tier); tier != "" && !strings.EqualFold(entity.Tier, tier) {
		return false
	}

	if labelKey := strings.TrimSpace(query.LabelKey); labelKey != "" {
		labelValue, ok := entityLabelValue(entity.Metadata.Labels, labelKey)
		if !ok || !strings.EqualFold(labelValue, strings.TrimSpace(query.LabelValue)) {
			return false
		}
	}

	for _, tagFilter := range normalizeStringSlice(query.Tags) {
		matched := false
		for _, tag := range entity.Metadata.Tags {
			if containsFold(tag, tagFilter) {
				matched = true
				break
			}
		}

		if !matched {
			return false
		}
	}

	return true
}

func entityLabelValue(labels map[string]string, key string) (string, bool) {
	if labels == nil {
		return "", false
	}
	if value, ok := labels[key]; ok {
		return value, true
	}
	for existingKey, value := range labels {
		if strings.EqualFold(existingKey, key) {
			return value, true
		}
	}
	return "", false
}

func matchesAlertConfigSearchTerm(
	rule AlertRule,
	entity alertGroupEntity,
	entityFound bool,
	searchTerm string,
) bool {
	if containsFold(rule.RuleName, searchTerm) {
		return true
	}

	if !entityFound {
		return false
	}

	if containsFold(entity.Name, searchTerm) ||
		containsFold(entity.Type, searchTerm) ||
		containsFold(entity.DataSourceName, searchTerm) {
		return true
	}

	for _, tag := range entity.Metadata.Tags {
		if containsFold(tag, searchTerm) {
			return true
		}
	}

	return false
}

func alertConfigRuleType(rule AlertRule) string {
	if strings.Contains(strings.ToLower(rule.Algorithm), alertConfigRuleTypeStatic) {
		return alertConfigRuleTypeStatic
	}

	return alertConfigRuleTypeAnomaly
}

func normalizeStringSlice(values []string) []string {
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}

		normalized = append(normalized, value)
	}

	return normalized
}

func containsFold(value, substring string) bool {
	if strings.TrimSpace(substring) == "" {
		return true
	}

	return strings.Contains(strings.ToLower(value), strings.ToLower(substring))
}

func formatAlertGroupLabels(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%s", key, labels[key]))
	}
	return strings.Join(parts, ", ")
}

func formatAlertConfigResponse(
	alertConfig AlertConfigResponse,
	entitiesByID map[string]alertGroupEntity,
	entityChannelsByID map[string][]NotificationChannel,
	notificationChannelsErr string,
	onlyWithoutNotificationChannel bool,
) string {
	header := fmt.Sprintf("Found %d alert rules:\n\n", len(alertConfig))
	if onlyWithoutNotificationChannel {
		header = fmt.Sprintf(
			"Found %d alert rule(s) with no per-entity notification channel configured:\n\n",
			len(alertConfig),
		)
	}
	formattedResponse := header
	for i, rule := range alertConfig {
		formattedResponse += fmt.Sprintf("Alert Rule %d:\n", i+1)
		formattedResponse += fmt.Sprintf("  ID: %s\n", rule.ID)
		formattedResponse += fmt.Sprintf("  Rule Name: %s\n", rule.RuleName)
		formattedResponse += fmt.Sprintf("  Primary Indicator: %s\n", rule.PrimaryIndicator)
		if rule.Expression != "" {
			formattedResponse += fmt.Sprintf("  Expression: %s\n", rule.Expression)
		}
		if rule.Condition != "" {
			formattedResponse += fmt.Sprintf("  Condition: %s\n", rule.Condition)
		}
		if rule.AlertCondition != "" {
			formattedResponse += fmt.Sprintf("  Alert Condition: %s\n", rule.AlertCondition)
		}
		if rule.EvalWindow > 0 {
			formattedResponse += fmt.Sprintf("  Eval Window: %d minutes\n", rule.EvalWindow)
		}
		if len(rule.ExpressionArgs) > 0 {
			formattedResponse += "  Indicators:\n"
			indicatorNames := make([]string, 0, len(rule.ExpressionArgs))
			for name := range rule.ExpressionArgs {
				indicatorNames = append(indicatorNames, name)
			}
			sort.Strings(indicatorNames)
			for _, indicatorName := range indicatorNames {
				arg := rule.ExpressionArgs[indicatorName]
				formattedResponse += fmt.Sprintf("    %s (KPI ID: %s)\n", indicatorName, arg.ID)
				if arg.LookupError != "" {
					formattedResponse += fmt.Sprintf("      PromQL: [lookup failed: %s]\n", arg.LookupError)
				} else if arg.PromQL != "" {
					formattedResponse += fmt.Sprintf("      PromQL: %s\n", arg.PromQL)
					if arg.Unit != "" {
						formattedResponse += fmt.Sprintf("      Unit: %s\n", arg.Unit)
					}
				}
				varKeys := make([]string, 0, len(arg.Variables))
				for k := range arg.Variables {
					varKeys = append(varKeys, k)
				}
				sort.Strings(varKeys)
				for _, k := range varKeys {
					formattedResponse += fmt.Sprintf("      Variable %s: %s\n", k, arg.Variables[k])
				}
			}
		}
		formattedResponse += fmt.Sprintf("  State: %s\n", rule.State)
		formattedResponse += fmt.Sprintf("  Severity: %s\n", rule.Severity)
		formattedResponse += fmt.Sprintf("  Algorithm: %s\n", rule.Algorithm)
		formattedResponse += fmt.Sprintf("  Entity ID: %s\n", rule.EntityID)

		if entity, ok := entitiesByID[rule.EntityID]; ok {
			formattedResponse += fmt.Sprintf("  Alert Group: %s\n", entity.Name)
			if entity.DataSourceName != "" {
				formattedResponse += fmt.Sprintf("  Data Source: %s\n", entity.DataSourceName)
			}
			if len(entity.Metadata.Tags) > 0 {
				formattedResponse += fmt.Sprintf("  Tags: %s\n", strings.Join(entity.Metadata.Tags, ", "))
			}
			if team := strings.TrimSpace(entity.Metadata.Team); team != "" {
				formattedResponse += fmt.Sprintf("  Team: %s\n", team)
			}
			if tier := strings.TrimSpace(entity.Tier); tier != "" {
				formattedResponse += fmt.Sprintf("  Tier: %s\n", tier)
			}
			if formatted := formatAlertGroupLabels(entity.Metadata.Labels); formatted != "" {
				formattedResponse += fmt.Sprintf("  Labels: %s\n", formatted)
			}
		}

		if notificationChannelsErr != "" {
			formattedResponse += fmt.Sprintf(
				"  Notification Channels: [lookup failed: %s]\n",
				notificationChannelsErr,
			)
		} else {
			bindings := entityChannelsByID[rule.EntityID]
			formattedResponse += fmt.Sprintf(
				"  Notification Channels: %s\n",
				formatNotificationChannelSummary(bindings),
			)
			formattedResponse += formatNotificationChannelBindingDetails(bindings)
		}

		if rule.ErrorSince != nil {
			errorTime := time.Unix(*rule.ErrorSince, 0).UTC().Format("2006-01-02 15:04:05 UTC")
			formattedResponse += fmt.Sprintf("  Error Since: %s\n", errorTime)
		}

		if len(rule.Properties) > 0 {
			formattedResponse += "  Properties:\n"
			propKeys := make([]string, 0, len(rule.Properties))
			for k := range rule.Properties {
				propKeys = append(propKeys, k)
			}
			sort.Strings(propKeys)
			for _, k := range propKeys {
				formattedResponse += fmt.Sprintf("    %s: %v\n", k, rule.Properties[k])
			}
		}

		createdTime := time.Unix(rule.CreatedAt, 0).UTC().Format("2006-01-02 15:04:05 UTC")
		updatedTime := time.Unix(rule.UpdatedAt, 0).UTC().Format("2006-01-02 15:04:05 UTC")
		formattedResponse += fmt.Sprintf("  Created: %s\n", createdTime)
		formattedResponse += fmt.Sprintf("  Updated: %s\n", updatedTime)
		formattedResponse += fmt.Sprintf("  Group Timeseries Notifications: %v\n", rule.GroupTimeseriesNotifications)
		formattedResponse += "\n"
	}

	return formattedResponse
}
