package entities

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"last9-mcp/internal/last9api"
)

const (
	pathAlertRules = "/entities/%s/alert-rules"
	pathAlertRule  = "/entities/%s/alert-rules/%s"
)

type AlertRuleInput struct {
	EntityID string
	RuleID   string
	Body     any
}

// AlertRule is the subset of a stored rule callers need to confirm which group owns it
// and to carry its annotations through edits.
type AlertRule struct {
	ID         string `json:"id"`
	EntityID   string `json:"entity_id"`
	Properties struct {
		Annotations map[string]any `json:"annotations"`
	} `json:"properties"`
}

// GetAlertRule calls GET /entities/{entity_id}/alert-rules/{rule_id}. The API looks the
// rule up by id alone, so callers must compare EntityID themselves.
func GetAlertRule(ctx context.Context, c *last9api.Client, in AlertRuleInput) (AlertRule, error) {
	var out AlertRule
	err := c.Do(ctx, "get alert rule", last9api.Request{Method: http.MethodGet, Path: alertRulePath(in)}, &out)
	return out, err
}

// CreateAlertRule calls POST /entities/{entity_id}/alert-rules and returns the raw response body.
func CreateAlertRule(ctx context.Context, c *last9api.Client, in AlertRuleInput) (json.RawMessage, error) {
	path := fmt.Sprintf(pathAlertRules, url.PathEscape(in.EntityID))
	return doAlertRule(ctx, c, "create alert rule", http.MethodPost, path, in.Body)
}

// UpdateAlertRule calls PUT /entities/{entity_id}/alert-rules/{rule_id} and returns the raw response body.
func UpdateAlertRule(ctx context.Context, c *last9api.Client, in AlertRuleInput) (json.RawMessage, error) {
	return doAlertRule(ctx, c, "update alert rule", http.MethodPut, alertRulePath(in), in.Body)
}

// PatchAlertRule calls PATCH /entities/{entity_id}/alert-rules/{rule_id}.
func PatchAlertRule(ctx context.Context, c *last9api.Client, in AlertRuleInput) error {
	return c.Do(ctx, "patch alert rule",
		last9api.Request{Method: http.MethodPatch, Path: alertRulePath(in), Body: in.Body}, nil)
}

// DeleteAlertRule calls DELETE /entities/{entity_id}/alert-rules/{rule_id} and returns the raw response body, if any.
func DeleteAlertRule(ctx context.Context, c *last9api.Client, in AlertRuleInput) (json.RawMessage, error) {
	return doAlertRule(ctx, c, "delete alert rule", http.MethodDelete, alertRulePath(in), nil)
}

func alertRulePath(in AlertRuleInput) string {
	return fmt.Sprintf(pathAlertRule, url.PathEscape(in.EntityID), url.PathEscape(in.RuleID))
}

// An empty body is a valid response (204 or no content); callers decide whether it is acceptable.
func doAlertRule(
	ctx context.Context, c *last9api.Client, op, method, path string, body any,
) (json.RawMessage, error) {
	var out json.RawMessage
	err := c.Do(ctx, op, last9api.Request{Method: method, Path: path, Body: body}, &out)
	if err != nil {
		return nil, err
	}
	return out, nil
}
