package alerting

import (
	"context"
	"net/http"
	"strings"

	"last9-mcp/internal/last9api"
	"last9-mcp/internal/last9api/entities"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type MuteAlertGroupArgs struct {
	EntityID string `json:"entity_id" jsonschema:"(Required) Alert group/entity UUID whose rules are all muted"`
	Until    int64  `json:"until" jsonschema:"(Required) Unix epoch seconds when the mute ends; 0 unmutes the group; a negative value such as -1 mutes indefinitely"`
}

func NewMuteAlertGroupHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, MuteAlertGroupArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args MuteAlertGroupArgs) (*mcp.CallToolResult, any, error) {
		if strings.TrimSpace(args.EntityID) == "" {
			return utils.ToolErrorResult("entity_id is required"), nil, nil
		}
		body, err := entities.SnoozeAlertRules(ctx, last9api.NewClient(client, cfg),
			entities.SnoozeAlertRulesInput{EntityID: args.EntityID, Until: args.Until})
		if err != nil {
			return nil, nil, err
		}
		return alertMutationResult(cfg, body, `{"muted":true}`), nil, nil
	}
}
