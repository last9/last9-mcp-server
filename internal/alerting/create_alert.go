package alerting

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"last9-mcp/internal/deeplink"
	"last9-mcp/internal/last9api"
	"last9-mcp/internal/last9api/entities"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// draftCreatedNote is the lifecycle sentence every alert create returns; evals match it verbatim.
const draftCreatedNote = "Created as a disabled draft; it will not fire. Promote it with patch_alert is_disabled=false only after the user confirms."

type CreateAlertArgs struct {
	EntityID string `json:"entity_id" jsonschema:"(Required) Alert group/entity UUID; discover it before creating the rule"`
	AlertRuleMutation
}

func NewCreateAlertHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, CreateAlertArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args CreateAlertArgs) (*mcp.CallToolResult, any, error) {
		if strings.TrimSpace(args.EntityID) == "" {
			return utils.ToolErrorResult("entity_id is required"), nil, nil
		}
		if err := validateAlertRulePayload(args.AlertRule); err != nil {
			return utils.ToolErrorResult(err.Error()), nil, nil
		}
		api := last9api.NewClient(client, cfg)
		if refusal, err := refuseChartDerivedKPI(ctx, api, requestSession(req), args); refusal != nil || err != nil {
			return refusal, nil, err
		}
		if refusal, err := refuseDuplicateRuleName(ctx, api, args.EntityID, args.AlertRule.RuleName); refusal != nil || err != nil {
			return refusal, nil, err
		}
		overridden := args.AlertRule.IsDisabled != nil && !*args.AlertRule.IsDisabled
		disabled := true
		args.AlertRule.IsDisabled = &disabled
		args.AlertRule.Properties = withCreatedViaAnnotations(args.AlertRule.Properties, newCreatedViaMarker(req))
		body, err := entities.CreateAlertRule(ctx, api,
			entities.AlertRuleInput{EntityID: args.EntityID, Body: args.AlertRule})
		if err != nil {
			return alertRuleWriteError(err)
		}
		response, err := parseMutationIDResponse(body)
		if err != nil {
			return nil, nil, utils.MarkUnusableWriteResponse(fmt.Errorf("invalid create alert response: %w", err))
		}
		text := strings.TrimSpace(string(body)) + "\n\n" + draftCreatedNote
		if overridden {
			text += " Requested is_disabled=false was ignored: create_alert always creates disabled."
		}
		return alertRuleMutationResult(cfg, text, deeplink.AlertRuleReference{RuleID: response.ID, GroupID: args.EntityID, GroupName: response.GroupName}), nil, nil
	}
}

// refuseDuplicateRuleName returns a tool error when the group already has a rule with this
// name. The API accepts duplicates, so a retried or repeated create would add a second rule.
func refuseDuplicateRuleName(ctx context.Context, api *last9api.Client, entityID string, ruleName *string) (*mcp.CallToolResult, error) {
	if ruleName == nil || strings.TrimSpace(*ruleName) == "" {
		return nil, nil
	}
	name := strings.TrimSpace(*ruleName)
	rules, err := entities.ListAlertRules(ctx, api, entityID)
	if err != nil {
		return alertGroupLookupError(err, entityID)
	}
	for _, rule := range rules {
		if strings.EqualFold(strings.TrimSpace(rule.RuleName), name) {
			return utils.ToolErrorResult(fmt.Sprintf("alert group %s already has a rule named %q (id %s); nothing was created. Refine it with update_alert or patch_alert, or choose another rule_name", entityID, rule.RuleName, rule.ID)), nil
		}
	}
	return nil, nil
}

// Sessions that described no chart skip the KPI lookup, so they pay no extra request.
func refuseChartDerivedKPI(ctx context.Context, api *last9api.Client, session *mcp.ServerSession, args CreateAlertArgs) (*mcp.CallToolResult, error) {
	if !hasDescribedChart(session) {
		return nil, nil
	}
	kpis, err := entities.ListEntityKPIs(ctx, api, args.EntityID)
	if err != nil {
		return alertGroupLookupError(err, args.EntityID)
	}
	for name, arg := range args.AlertRule.ExpressionArgs {
		for _, kpi := range kpis {
			if !strings.EqualFold(kpi.ID, arg.ID) {
				continue
			}
			if refusal := chartQueryRefusal(session, fmt.Sprintf("KPI %q (id %s) bound as %s", kpi.Name, kpi.ID, name), kpi.Definition.Query); refusal != "" {
				return utils.ToolErrorResult(refusal), nil
			}
		}
	}
	return nil, nil
}

func alertGroupLookupError(err error, entityID string) (*mcp.CallToolResult, error) {
	var upstream *utils.UpstreamHTTPError
	if errors.As(err, &upstream) && (upstream.StatusCode == http.StatusBadRequest || upstream.StatusCode == http.StatusNotFound) {
		// The group lists take only entity_id, so their 4xx means the group is unknown.
		return utils.ToolErrorResult(fmt.Sprintf("entity_id %q is not an alert group; nothing was created. Copy the alert group id from get_alert_groups", entityID)), nil
	}
	return nil, fmt.Errorf("%w Nothing was created.", withUpstreamBody(err))
}
