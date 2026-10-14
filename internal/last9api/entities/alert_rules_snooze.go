package entities

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"last9-mcp/internal/last9api"
)

const pathAlertRulesSnooze = "/entities/%s/alert-rules/snooze"

type SnoozeAlertRulesInput struct {
	EntityID string
	Until    int64
}

type snoozeBody struct {
	Until int64 `json:"until"`
}

// SnoozeAlertRules calls POST /entities/{entity_id}/alert-rules/snooze and returns the raw response body.
func SnoozeAlertRules(ctx context.Context, c *last9api.Client, in SnoozeAlertRulesInput) (json.RawMessage, error) {
	path := fmt.Sprintf(pathAlertRulesSnooze, url.PathEscape(in.EntityID))
	return doAlertRule(ctx, c, "mute alert group", http.MethodPost, path, snoozeBody{Until: in.Until})
}
