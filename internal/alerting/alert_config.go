package alerting

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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
)

type alertGroupEntity struct {
	ID             string                   `json:"id"`
	Name           string                   `json:"name"`
	Type           string                   `json:"type"`
	DataSourceName string                   `json:"data_source_name"`
	Metadata       alertGroupEntityMetadata `json:"metadata"`
}

type alertGroupEntityMetadata struct {
	Tags []string `json:"tags"`
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
	ruleType := strings.ToLower(strings.TrimSpace(args.RuleType))
	if ruleType == "" {
		return nil
	}

	if ruleType != alertConfigRuleTypeStatic && ruleType != alertConfigRuleTypeAnomaly {
		return fmt.Errorf("rule_type must be one of %q or %q", alertConfigRuleTypeStatic, alertConfigRuleTypeAnomaly)
	}

	return nil
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
	args GetAlertConfigArgs,
) (map[string]alertGroupEntity, error) {
	requestBody := filterAlertGroupEntitiesRequest{
		Filters: buildAlertGroupEntityLookupFilters(args),
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

func buildAlertGroupEntityLookupFilters(args GetAlertConfigArgs) []alertGroupEntityFilter {
	explicitFilters := make([]alertGroupEntityFilter, 0, 2+len(normalizeStringSlice(args.Tags)))

	if alertGroupName := strings.TrimSpace(args.AlertGroupName); alertGroupName != "" {
		explicitFilters = append(explicitFilters, newAlertGroupEntityFilter(
			entityFilterEntityName,
			alertGroupName,
			entityFilterContains,
		))
	}

	if alertGroupType := strings.TrimSpace(args.AlertGroupType); alertGroupType != "" {
		explicitFilters = append(explicitFilters, newAlertGroupEntityFilter(
			entityFilterEntityType,
			alertGroupType,
			entityFilterContains,
		))
	}

	if dataSourceName := strings.TrimSpace(args.DataSourceName); dataSourceName != "" {
		explicitFilters = append(explicitFilters, newAlertGroupEntityFilter(
			entityFilterDataSource,
			dataSourceName,
			entityFilterContains,
		))
	}

	for _, tag := range normalizeStringSlice(args.Tags) {
		explicitFilters = append(explicitFilters, newAlertGroupEntityFilter(
			entityFilterTags,
			tag,
			entityFilterContains,
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
	entityIDs := make(map[string]struct{}, len(args.EntityIDs))
	for _, entityID := range normalizeStringSlice(args.EntityIDs) {
		entityIDs[entityID] = struct{}{}
	}

	filtered := make(AlertConfigResponse, 0, len(alertConfig))
	for _, rule := range alertConfig {
		if ruleName := strings.TrimSpace(args.RuleName); ruleName != "" && !containsFold(rule.RuleName, ruleName) {
			continue
		}

		if severity := strings.TrimSpace(args.Severity); severity != "" && !strings.EqualFold(rule.Severity, severity) {
			continue
		}

		if ruleType := strings.TrimSpace(args.RuleType); ruleType != "" && !strings.EqualFold(alertConfigRuleType(rule), ruleType) {
			continue
		}

		if algorithm := strings.TrimSpace(args.Algorithm); algorithm != "" && !strings.EqualFold(rule.Algorithm, algorithm) {
			continue
		}

		if state := strings.TrimSpace(args.State); state != "" && !strings.EqualFold(rule.State, state) {
			continue
		}

		if len(entityIDs) > 0 {
			if _, ok := entityIDs[rule.EntityID]; !ok {
				continue
			}
		}

		if externalRef := strings.TrimSpace(args.ExternalRef); externalRef != "" && !containsFold(rule.ExternalRef, externalRef) {
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

		if !matchesAlertGroupEntityFilters(entity, entityFound, args) {
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
		strings.TrimSpace(args.AlertGroupName) != "" ||
		strings.TrimSpace(args.AlertGroupType) != "" ||
		strings.TrimSpace(args.DataSourceName) != "" ||
		len(normalizeStringSlice(args.Tags)) > 0
}

func matchesAlertGroupEntityFilters(
	entity alertGroupEntity,
	entityFound bool,
	args GetAlertConfigArgs,
) bool {
	hasTypedEntityFilters := strings.TrimSpace(args.AlertGroupName) != "" ||
		strings.TrimSpace(args.AlertGroupType) != "" ||
		strings.TrimSpace(args.DataSourceName) != "" ||
		len(normalizeStringSlice(args.Tags)) > 0
	if !hasTypedEntityFilters {
		return true
	}

	if !entityFound {
		return false
	}

	if alertGroupName := strings.TrimSpace(args.AlertGroupName); alertGroupName != "" && !containsFold(entity.Name, alertGroupName) {
		return false
	}

	if alertGroupType := strings.TrimSpace(args.AlertGroupType); alertGroupType != "" && !containsFold(entity.Type, alertGroupType) {
		return false
	}

	if dataSourceName := strings.TrimSpace(args.DataSourceName); dataSourceName != "" && !containsFold(entity.DataSourceName, dataSourceName) {
		return false
	}

	for _, tagFilter := range normalizeStringSlice(args.Tags) {
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

func matchesAlertConfigSearchTerm(
	rule AlertRule,
	entity alertGroupEntity,
	entityFound bool,
	searchTerm string,
) bool {
	if containsFold(rule.RuleName, searchTerm) ||
		containsFold(rule.ExternalRef, searchTerm) ||
		containsFold(rule.PrimaryIndicator, searchTerm) {
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

func formatAlertConfigResponse(alertConfig AlertConfigResponse) string {
	formattedResponse := fmt.Sprintf("Found %d alert rules:\n\n", len(alertConfig))
	for i, rule := range alertConfig {
		formattedResponse += fmt.Sprintf("Alert Rule %d:\n", i+1)
		formattedResponse += fmt.Sprintf("  ID: %s\n", rule.ID)
		formattedResponse += fmt.Sprintf("  Rule Name: %s\n", rule.RuleName)
		formattedResponse += fmt.Sprintf("  Primary Indicator: %s\n", rule.PrimaryIndicator)
		formattedResponse += fmt.Sprintf("  State: %s\n", rule.State)
		formattedResponse += fmt.Sprintf("  Severity: %s\n", rule.Severity)
		formattedResponse += fmt.Sprintf("  Algorithm: %s\n", rule.Algorithm)
		formattedResponse += fmt.Sprintf("  Entity ID: %s\n", rule.EntityID)

		if rule.ErrorSince != nil {
			errorTime := time.Unix(*rule.ErrorSince, 0).UTC().Format("2006-01-02 15:04:05 UTC")
			formattedResponse += fmt.Sprintf("  Error Since: %s\n", errorTime)
		}

		if len(rule.Properties) > 0 {
			formattedResponse += "  Properties:\n"
			for k, v := range rule.Properties {
				formattedResponse += fmt.Sprintf("    %s: %v\n", k, v)
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
