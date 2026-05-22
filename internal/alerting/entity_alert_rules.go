package alerting

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"last9-mcp/internal/constants"
	"last9-mcp/internal/deeplink"
	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const GetEntityAlertRulesDescription = `Fetches all alert rules for a specific entity (alert group) with full detail:
expression, condition, eval window, and resolved PromQL for each indicator.

Use this tool when you need the complete alert rule configuration for a known entity,
including the actual PromQL queries behind each indicator. The org-wide get_alert_config
tool omits expression_args and PromQL; this tool includes them.

Input:
- entity_id (required): UUID of the entity / alert group
- severity (optional): filter rules by severity (e.g. "breach", "warn")

Output per rule:
- id, rule_name, primary_indicator, state, severity, algorithm
- expression, condition, alert_condition, eval_window
- indicators: each indicator name with resolved PromQL, unit, and variables
- created_at, updated_at`

// GetEntityAlertRulesArgs holds the input arguments for get_entity_alert_rules.
type GetEntityAlertRulesArgs struct {
	EntityID string `json:"entity_id"`
	Severity string `json:"severity,omitempty"`
}

// NewGetEntityAlertRulesHandler returns the MCP tool handler for get_entity_alert_rules.
func NewGetEntityAlertRulesHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, GetEntityAlertRulesArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args GetEntityAlertRulesArgs) (*mcp.CallToolResult, any, error) {
		entityID := strings.TrimSpace(args.EntityID)
		if entityID == "" {
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: "entity_id is required"}},
				IsError: true,
			}, nil, nil
		}

		rules, err := fetchEntityAlertRules(ctx, client, cfg, entityID)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to fetch entity alert rules: %w", err)
		}

		if severity := strings.TrimSpace(args.Severity); severity != "" {
			filtered := make(AlertConfigResponse, 0, len(rules))
			for _, r := range rules {
				if strings.EqualFold(r.Severity, severity) {
					filtered = append(filtered, r)
				}
			}
			rules = filtered
		}

		resolveAlertConfigKPIs(ctx, client, cfg, rules)

		dlBuilder := deeplink.NewBuilder(cfg.OrgSlug, cfg.ClusterID)
		dashboardURL := dlBuilder.BuildAlertingGroupsLink()

		return &mcp.CallToolResult{
			Meta: deeplink.ToMeta(dashboardURL),
			Content: []mcp.Content{
				&mcp.TextContent{Text: formatAlertConfigResponse(rules)},
			},
		}, nil, nil
	}
}

func fetchEntityAlertRules(
	ctx context.Context,
	client *http.Client,
	cfg models.Config,
	entityID string,
) (AlertConfigResponse, error) {
	fullURL := cfg.APIBaseURL + fmt.Sprintf(constants.EndpointEntityAlertRules, entityID)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set(constants.HeaderAccept, constants.HeaderAcceptJSON)
	httpReq.Header.Set(constants.HeaderXLast9APIToken, constants.BearerPrefix+cfg.TokenManager.GetAccessToken(ctx))

	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API returned %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var wrapper struct {
		Rules AlertConfigResponse `json:"rules"`
	}
	if err := json.Unmarshal(body, &wrapper); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return wrapper.Rules, nil
}
