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
		overridden := args.AlertRule.IsDisabled != nil && !*args.AlertRule.IsDisabled
		disabled := true
		args.AlertRule.IsDisabled = &disabled
		args.AlertRule.Properties = withCreatedViaAnnotations(args.AlertRule.Properties, newCreatedViaMarker(req))
		body, err := entities.CreateAlertRule(ctx, last9api.NewClient(client, cfg),
			entities.AlertRuleInput{EntityID: args.EntityID, Body: args.AlertRule})
		if err != nil {
			return alertRuleWriteError(err)
		}
		if err := validateMutationIDResponse(body); err != nil {
			return nil, nil, fmt.Errorf("invalid create alert response: %w", err)
		}
		text := strings.TrimSpace(string(body)) + "\n\nCreated as a disabled draft; it will not fire. Promote it with patch_alert is_disabled=false only after the user confirms."
		if overridden {
			text += " Requested is_disabled=false was ignored: create_alert always creates disabled."
		}
		return alertMutationResult(cfg, []byte(text), ""), nil, nil
	}
}
