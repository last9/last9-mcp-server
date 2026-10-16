package alerting

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"last9-mcp/internal/last9api"
	"last9-mcp/internal/last9api/entities"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type UpdateAlertArgs struct {
	EntityID string `json:"entity_id" jsonschema:"(Required) Alert group/entity UUID that owns the rule"`
	ID       string `json:"id" jsonschema:"(Required) Alert rule UUID returned by get_alert_config; update may return a replacement UUID"`
	AlertRuleMutation
}

func NewUpdateAlertHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, UpdateAlertArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args UpdateAlertArgs) (*mcp.CallToolResult, any, error) {
		if strings.TrimSpace(args.EntityID) == "" {
			return utils.ToolErrorResult("entity_id is required"), nil, nil
		}
		if err := validateAlertRuleID(args.ID); err != nil {
			return utils.ToolErrorResult(err.Error()), nil, nil
		}
		if err := validateAlertRulePayload(args.AlertRule); err != nil {
			return utils.ToolErrorResult(err.Error()), nil, nil
		}
		api := last9api.NewClient(client, cfg)
		if refusal, err := requireRuleInGroup(ctx, api, args.EntityID, args.ID); refusal != nil || err != nil {
			return refusal, nil, err
		}
		body, err := entities.UpdateAlertRule(ctx, api,
			entities.AlertRuleInput{EntityID: args.EntityID, RuleID: args.ID, Body: args.AlertRule})
		if err != nil {
			return nil, nil, withUpstreamBody(err)
		}
		if err := validateMutationIDResponse(body); err != nil {
			return nil, nil, fmt.Errorf("invalid update alert response: %w", err)
		}
		return alertMutationResult(cfg, body, ""), nil, nil
	}
}
