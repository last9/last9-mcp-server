package alerting

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

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
		if err := validateMutationIDResponse(body); err != nil {
			return nil, nil, fmt.Errorf("invalid create alert response: %w", err)
		}
		text := strings.TrimSpace(string(body)) + "\n\n" + draftCreatedNote
		if overridden {
			text += " Requested is_disabled=false was ignored: create_alert always creates disabled."
		}
		return alertMutationResult(cfg, []byte(text), ""), nil, nil
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
	var upstream *utils.UpstreamHTTPError
	if errors.As(err, &upstream) && (upstream.StatusCode == http.StatusBadRequest || upstream.StatusCode == http.StatusNotFound) {
		// The list takes only entity_id, so its 4xx means the group is unknown.
		return utils.ToolErrorResult(fmt.Sprintf("entity_id %q is not an alert group; nothing was created. Copy the alert group id from get_alert_groups", entityID)), nil
	}
	if err != nil {
		return nil, withUpstreamBody(err)
	}
	for _, rule := range rules {
		if strings.EqualFold(strings.TrimSpace(rule.RuleName), name) {
			return utils.ToolErrorResult(fmt.Sprintf("alert group %s already has a rule named %q (id %s); nothing was created. Refine it with update_alert or patch_alert, or choose another rule_name", entityID, rule.RuleName, rule.ID)), nil
		}
	}
	return nil, nil
}
