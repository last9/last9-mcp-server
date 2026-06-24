package alerting

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"last9-mcp/internal/constants"
	"last9-mcp/internal/deeplink"
	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

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
			return toolErrorResult("entity_id is required"), nil, nil
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
				&mcp.TextContent{Text: formatEntityAlertRulesResponse(rules)},
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

// formatEntityAlertRulesResponse formats entity-scoped rules showing only
// expression/indicator fields. Basic metadata (state, severity, timestamps, etc.)
// is omitted because callers already have it from get_alert_config.
func formatEntityAlertRulesResponse(rules AlertConfigResponse) string {
	out := fmt.Sprintf("Found %d alert rules for entity:\n\n", len(rules))
	for i, rule := range rules {
		out += fmt.Sprintf("Rule %d:\n", i+1)
		out += fmt.Sprintf("  ID: %s\n", rule.ID)
		out += fmt.Sprintf("  Rule Name: %s\n", rule.RuleName)
		if rule.Expression != "" {
			out += fmt.Sprintf("  Expression: %s\n", rule.Expression)
		}
		if rule.Condition != "" {
			out += fmt.Sprintf("  Condition: %s\n", rule.Condition)
		}
		if rule.AlertCondition != "" {
			out += fmt.Sprintf("  Alert Condition: %s\n", rule.AlertCondition)
		}
		if rule.EvalWindow > 0 {
			out += fmt.Sprintf("  Eval Window: %d minutes\n", rule.EvalWindow)
		}
		if len(rule.ExpressionArgs) > 0 {
			out += "  Indicators:\n"
			indicatorNames := make([]string, 0, len(rule.ExpressionArgs))
			for name := range rule.ExpressionArgs {
				indicatorNames = append(indicatorNames, name)
			}
			sort.Strings(indicatorNames)
			for _, name := range indicatorNames {
				arg := rule.ExpressionArgs[name]
				out += fmt.Sprintf("    %s (KPI ID: %s)\n", name, arg.ID)
				if arg.LookupError != "" {
					out += fmt.Sprintf("      PromQL: [lookup failed: %s]\n", arg.LookupError)
				} else if arg.PromQL != "" {
					out += fmt.Sprintf("      PromQL: %s\n", arg.PromQL)
					if arg.Unit != "" {
						out += fmt.Sprintf("      Unit: %s\n", arg.Unit)
					}
				}
				varKeys := make([]string, 0, len(arg.Variables))
				for k := range arg.Variables {
					varKeys = append(varKeys, k)
				}
				sort.Strings(varKeys)
				for _, k := range varKeys {
					out += fmt.Sprintf("      Variable %s: %s\n", k, arg.Variables[k])
				}
			}
		}
		out += "\n"
	}
	return out
}
