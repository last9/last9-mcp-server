package alerting

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"last9-mcp/internal/last9api"
	"last9-mcp/internal/last9api/entities"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type ValidateAlertExpressionArgs struct {
	EntityID       string            `json:"entity_id" jsonschema:"(Required) Alert group/entity UUID whose KPIs the expression uses"`
	Expression     string            `json:"expression" jsonschema:"(Required) Arithmetic expression over KPI names, for example errors / requests"`
	IndicatorsUsed map[string]string `json:"indicators_used" jsonschema:"(Required) Non-empty map of each KPI name used in the expression to its KPI UUID owned by entity_id, for example {\"errors\":\"<kpi-uuid>\"}; every name in the expression must be present. Use each KPI's exact name: this check accepts any name, but create_alert and update_alert reject other names"`
}

func NewValidateAlertExpressionHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, ValidateAlertExpressionArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args ValidateAlertExpressionArgs) (*mcp.CallToolResult, any, error) {
		if strings.TrimSpace(args.EntityID) == "" {
			return utils.ToolErrorResult("entity_id is required"), nil, nil
		}
		if strings.TrimSpace(args.Expression) == "" {
			return utils.ToolErrorResult("expression is required"), nil, nil
		}
		if len(args.IndicatorsUsed) == 0 {
			return utils.ToolErrorResult("indicators_used is required and must map each indicator name in the expression to its KPI UUID"), nil, nil
		}
		body, err := entities.ValidateAlertExpression(ctx, last9api.NewClient(client, cfg), entities.ValidateExpressionInput{
			EntityID: args.EntityID, Expression: args.Expression, IndicatorsUsed: args.IndicatorsUsed,
		})
		var apiErr *utils.UpstreamHTTPError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusUnprocessableEntity && apiErr.Body != "" {
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: apiErr.Body}}}, nil, nil
		}
		if err != nil {
			return nil, nil, err
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(body)}}}, nil, nil
	}
}
