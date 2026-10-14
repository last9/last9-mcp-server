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

type DeleteAlertArgs struct {
	EntityID string `json:"entity_id" jsonschema:"(Required) Alert group/entity UUID that owns the rule"`
	ID       string `json:"id" jsonschema:"(Required) Alert rule UUID returned by get_alert_config"`
}

func NewDeleteAlertHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, DeleteAlertArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args DeleteAlertArgs) (*mcp.CallToolResult, any, error) {
		if strings.TrimSpace(args.EntityID) == "" {
			return utils.ToolErrorResult("entity_id is required"), nil, nil
		}
		if err := validateAlertRuleID(args.ID); err != nil {
			return utils.ToolErrorResult(err.Error()), nil, nil
		}
		body, err := entities.DeleteAlertRule(ctx, last9api.NewClient(client, cfg),
			entities.AlertRuleInput{EntityID: args.EntityID, RuleID: args.ID})
		if err != nil {
			return nil, nil, err
		}
		return alertMutationResult(cfg, body, fmt.Sprintf(`{"deleted":true,"id":%q}`, args.ID)), nil, nil
	}
}
