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

type PatchAlertArgs struct {
	EntityID   string           `json:"entity_id" jsonschema:"(Required) Alert group/entity UUID that owns the rule"`
	ID         string           `json:"id" jsonschema:"(Required) Alert rule UUID"`
	IsDisabled *bool            `json:"is_disabled,omitempty" jsonschema:"Set false to promote/enable or true to pause/disable"`
	MuteUntil  *int64           `json:"mute_until,omitempty" jsonschema:"Unix epoch mute deadline; 0 unmutes when the entity itself is not snoozed"`
	Properties *AlertProperties `json:"properties,omitempty" jsonschema:"Replace responder description, runbook, annotations, and query mode without recreating the rule"`
}

type alertPatchBody struct {
	IsDisabled *bool            `json:"is_disabled,omitempty"`
	MuteUntil  *int64           `json:"mute_until,omitempty"`
	Properties *AlertProperties `json:"properties,omitempty"`
}

func NewPatchAlertHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, PatchAlertArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args PatchAlertArgs) (*mcp.CallToolResult, any, error) {
		if strings.TrimSpace(args.EntityID) == "" {
			return utils.ToolErrorResult("entity_id is required"), nil, nil
		}
		if err := validateAlertRuleID(args.ID); err != nil {
			return utils.ToolErrorResult(err.Error()), nil, nil
		}
		if args.IsDisabled == nil && args.MuteUntil == nil && args.Properties == nil {
			return utils.ToolErrorResult("at least one of is_disabled, mute_until, or properties is required"), nil, nil
		}
		err := entities.PatchAlertRule(ctx, last9api.NewClient(client, cfg), entities.AlertRuleInput{
			EntityID: args.EntityID,
			RuleID:   args.ID,
			Body:     alertPatchBody{args.IsDisabled, args.MuteUntil, args.Properties},
		})
		if err != nil {
			return nil, nil, err
		}
		return alertMutationResult(cfg, nil, fmt.Sprintf(`{"patched":true,"id":%q}`, args.ID)), nil, nil
	}
}
