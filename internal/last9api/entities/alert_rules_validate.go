package entities

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"last9-mcp/internal/last9api"
)

const pathAlertRulesValidateExpression = "/entities/%s/alert-rules/validate_expression"

type ValidateExpressionInput struct {
	EntityID       string
	Expression     string
	IndicatorsUsed map[string]string
}

type validateExpressionBody struct {
	Expression     string            `json:"expression"`
	IndicatorsUsed map[string]string `json:"indicators_used"`
}

// ValidateAlertExpression calls POST /entities/{entity_id}/alert-rules/validate_expression and returns the raw response body.
func ValidateAlertExpression(ctx context.Context, c *last9api.Client, in ValidateExpressionInput) (json.RawMessage, error) {
	indicators := in.IndicatorsUsed
	if indicators == nil {
		indicators = map[string]string{}
	}
	path := fmt.Sprintf(pathAlertRulesValidateExpression, url.PathEscape(in.EntityID))
	return doAlertRule(ctx, c, "validate alert expression", http.MethodPost, path,
		validateExpressionBody{Expression: in.Expression, IndicatorsUsed: indicators})
}
