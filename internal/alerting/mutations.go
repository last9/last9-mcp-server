package alerting

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"last9-mcp/internal/constants"
	"last9-mcp/internal/deeplink"
	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const maxAlertMutationResponseBytes int64 = 1024 * 1024

type AlertExpressionArg struct {
	ID                string            `json:"id" jsonschema:"(Required) KPI UUID owned by the alert group"`
	Variables         map[string]string `json:"variables,omitempty" jsonschema:"Grafana template variable values"`
	VariableOperators map[string]string `json:"variable_operators,omitempty" jsonschema:"Optional matcher override per variable: =, !=, =~, or !~"`
}

type AlertRunbook struct {
	Link string `json:"link" jsonschema:"Runbook URL; annotation templates are supported"`
}

type AlertProperties struct {
	Description string            `json:"description,omitempty" jsonschema:"Responder-facing symptom, impact, and intended action"`
	Runbook     *AlertRunbook     `json:"runbook,omitempty" jsonschema:"Runbook link for responders"`
	Annotations map[string]string `json:"annotations,omitempty" jsonschema:"Responder context and links; values may use Last9 annotation templates"`
	QueryMode   string            `json:"query_mode,omitempty" jsonschema:"Query authoring mode: builder or code"`
}

type AlertRulePayload struct {
	PrimaryIndicator             string                        `json:"primary_indicator" jsonschema:"(Required) Primary KPI name; must be a key in expression_args"`
	ExpressionArgs               map[string]AlertExpressionArg `json:"expression_args" jsonschema:"(Required) Indicator name to KPI binding; all KPIs must belong to entity_id"`
	Expression                   *string                       `json:"expression,omitempty" jsonschema:"Rule expression. Supported anomaly macros determine the algorithm; clients cannot set algorithm directly."`
	Condition                    *string                       `json:"condition,omitempty" jsonschema:"Required for static rules; boolean expression over expr, for example expr > 100"`
	AlertCondition               *string                       `json:"alert_condition,omitempty" jsonschema:"Required for static rules; firing window expression, for example count_true(result) >= 3"`
	EvalWindow                   *int                          `json:"eval_window,omitempty" jsonschema:"Required for static rules; evaluation window in minutes, range 0-60"`
	IsDisabled                   *bool                         `json:"is_disabled,omitempty" jsonschema:"Create disabled for validation, then promote with patch_alert"`
	ExternalRef                  *string                       `json:"external_ref,omitempty" jsonschema:"Optional alphanumeric-and-hyphen external reference"`
	Severity                     *string                       `json:"severity,omitempty" jsonschema:"Severity: breach or threat; backend chooses an algorithm default when omitted"`
	RuleName                     *string                       `json:"rule_name,omitempty" jsonschema:"Alert rule name; backend generates one when omitted"`
	MuteUntil                    *int64                        `json:"mute_until,omitempty" jsonschema:"Unix epoch mute deadline; use patch_alert for lifecycle-only changes"`
	Properties                   *AlertProperties              `json:"properties,omitempty" jsonschema:"Description, runbook, annotations, and query mode"`
	GroupTimeseriesNotifications *bool                         `json:"group_timeseries_notifications,omitempty" jsonschema:"Group notifications across matching time series"`
}

type AlertRuleMutation struct {
	AlertRule AlertRulePayload `json:"alert_rule" jsonschema:"(Required) Complete alert-rule configuration"`
}

type CreateAlertArgs struct {
	EntityID string `json:"entity_id" jsonschema:"(Required) Alert group/entity UUID; discover it before creating the rule"`
	AlertRuleMutation
}

type UpdateAlertArgs struct {
	EntityID string `json:"entity_id" jsonschema:"(Required) Alert group/entity UUID that owns the rule"`
	ID       string `json:"id" jsonschema:"(Required) Alert rule UUID returned by get_alert_config; update may return a replacement UUID"`
	AlertRuleMutation
}

type DeleteAlertArgs struct {
	EntityID string `json:"entity_id" jsonschema:"(Required) Alert group/entity UUID that owns the rule"`
	ID       string `json:"id" jsonschema:"(Required) Alert rule UUID returned by get_alert_config"`
}

type PatchAlertArgs struct {
	EntityID   string           `json:"entity_id" jsonschema:"(Required) Alert group/entity UUID that owns the rule"`
	ID         string           `json:"id" jsonschema:"(Required) Alert rule UUID"`
	IsDisabled *bool            `json:"is_disabled,omitempty" jsonschema:"Set false to promote/enable or true to pause/disable"`
	MuteUntil  *int64           `json:"mute_until,omitempty" jsonschema:"Unix epoch mute deadline; 0 unmutes when the entity itself is not snoozed"`
	Properties *AlertProperties `json:"properties,omitempty" jsonschema:"Replace responder description, runbook, annotations, and query mode without recreating the rule"`
}

func NewCreateAlertHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, CreateAlertArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args CreateAlertArgs) (*mcp.CallToolResult, any, error) {
		if strings.TrimSpace(args.EntityID) == "" {
			return toolErrorResult("entity_id is required"), nil, nil
		}
		if err := validateAlertRulePayload(args.AlertRule); err != nil {
			return toolErrorResult(err.Error()), nil, nil
		}
		payload, err := marshalAlertRulePayload(args.AlertRule)
		if err != nil {
			return nil, nil, err
		}
		path := fmt.Sprintf(constants.EndpointEntityAlertRules, url.PathEscape(args.EntityID))
		body, err := doAlertMutation(ctx, client, cfg, http.MethodPost, path, payload)
		if err != nil {
			return nil, nil, err
		}
		if err := validateMutationIDResponse(body); err != nil {
			return nil, nil, fmt.Errorf("invalid create alert response: %w", err)
		}
		return alertMutationResult(cfg, body, ""), nil, nil
	}
}

func NewUpdateAlertHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, UpdateAlertArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args UpdateAlertArgs) (*mcp.CallToolResult, any, error) {
		if strings.TrimSpace(args.EntityID) == "" {
			return toolErrorResult("entity_id is required"), nil, nil
		}
		if err := validateAlertRuleID(args.ID); err != nil {
			return toolErrorResult(err.Error()), nil, nil
		}
		if err := validateAlertRulePayload(args.AlertRule); err != nil {
			return toolErrorResult(err.Error()), nil, nil
		}
		path := fmt.Sprintf(constants.EndpointEntityAlertRuleByID, url.PathEscape(args.EntityID), url.PathEscape(args.ID))
		payload, err := marshalAlertRulePayload(args.AlertRule)
		if err != nil {
			return nil, nil, err
		}
		body, err := doAlertMutation(ctx, client, cfg, http.MethodPut, path, payload)
		if err != nil {
			return nil, nil, err
		}
		if err := validateMutationIDResponse(body); err != nil {
			return nil, nil, fmt.Errorf("invalid update alert response: %w", err)
		}
		return alertMutationResult(cfg, body, ""), nil, nil
	}
}

func NewPatchAlertHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, PatchAlertArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args PatchAlertArgs) (*mcp.CallToolResult, any, error) {
		if strings.TrimSpace(args.EntityID) == "" {
			return toolErrorResult("entity_id is required"), nil, nil
		}
		if err := validateAlertRuleID(args.ID); err != nil {
			return toolErrorResult(err.Error()), nil, nil
		}
		if args.IsDisabled == nil && args.MuteUntil == nil && args.Properties == nil {
			return toolErrorResult("at least one of is_disabled, mute_until, or properties is required"), nil, nil
		}
		payload, err := json.Marshal(struct {
			IsDisabled *bool            `json:"is_disabled,omitempty"`
			MuteUntil  *int64           `json:"mute_until,omitempty"`
			Properties *AlertProperties `json:"properties,omitempty"`
		}{args.IsDisabled, args.MuteUntil, args.Properties})
		if err != nil {
			return nil, nil, fmt.Errorf("failed to encode alert patch: %w", err)
		}
		path := fmt.Sprintf(constants.EndpointEntityAlertRuleByID, url.PathEscape(args.EntityID), url.PathEscape(args.ID))
		if _, err := doAlertMutation(ctx, client, cfg, http.MethodPatch, path, payload); err != nil {
			return nil, nil, err
		}
		return alertMutationResult(cfg, nil, fmt.Sprintf(`{"patched":true,"id":%q}`, args.ID)), nil, nil
	}
}

func NewDeleteAlertHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, DeleteAlertArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args DeleteAlertArgs) (*mcp.CallToolResult, any, error) {
		if strings.TrimSpace(args.EntityID) == "" {
			return toolErrorResult("entity_id is required"), nil, nil
		}
		if err := validateAlertRuleID(args.ID); err != nil {
			return toolErrorResult(err.Error()), nil, nil
		}
		path := fmt.Sprintf(constants.EndpointEntityAlertRuleByID, url.PathEscape(args.EntityID), url.PathEscape(args.ID))
		body, err := doAlertMutation(ctx, client, cfg, http.MethodDelete, path, nil)
		if err != nil {
			return nil, nil, err
		}
		return alertMutationResult(cfg, body, fmt.Sprintf(`{"deleted":true,"id":%q}`, args.ID)), nil, nil
	}
}

func validateAlertRuleID(id string) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("id is required")
	}
	return nil
}

func validateAlertRulePayload(rule AlertRulePayload) error {
	if strings.TrimSpace(rule.PrimaryIndicator) == "" {
		return fmt.Errorf("alert_rule.primary_indicator is required")
	}
	if len(rule.ExpressionArgs) == 0 {
		return fmt.Errorf("alert_rule.expression_args is required")
	}
	arg, ok := rule.ExpressionArgs[rule.PrimaryIndicator]
	if !ok {
		return fmt.Errorf("alert_rule.primary_indicator must be present in expression_args")
	}
	if strings.TrimSpace(arg.ID) == "" {
		return fmt.Errorf("alert_rule.expression_args[%q].id is required", rule.PrimaryIndicator)
	}
	if rule.EvalWindow != nil && (*rule.EvalWindow < 0 || *rule.EvalWindow > 60) {
		return fmt.Errorf("alert_rule.eval_window must be between 0 and 60 minutes")
	}
	if rule.Severity != nil && *rule.Severity != "breach" && *rule.Severity != "threat" {
		return fmt.Errorf("alert_rule.severity must be breach or threat")
	}
	if rule.Properties != nil && rule.Properties.QueryMode != "" && rule.Properties.QueryMode != "builder" && rule.Properties.QueryMode != "code" {
		return fmt.Errorf("alert_rule.properties.query_mode must be builder or code")
	}
	return nil
}

func marshalAlertRulePayload(rule AlertRulePayload) ([]byte, error) {
	payload, err := json.Marshal(rule)
	if err != nil {
		return nil, fmt.Errorf("failed to encode alert_rule: %w", err)
	}
	return payload, nil
}

func validateMutationIDResponse(body []byte) error {
	var response struct {
		ID string `json:"id"`
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return fmt.Errorf("API returned an empty body; replacement rule ID is unknown")
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}
	if strings.TrimSpace(response.ID) == "" {
		return fmt.Errorf("API response is missing id")
	}
	return nil
}

func doAlertMutation(ctx context.Context, client *http.Client, cfg models.Config, method, path string, payload []byte) ([]byte, error) {
	var body io.Reader
	if payload != nil {
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, cfg.APIBaseURL+path, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create alert rule request: %w", err)
	}
	req.Header.Set(constants.HeaderAccept, constants.HeaderAcceptJSON)
	if payload != nil {
		req.Header.Set(constants.HeaderContentType, constants.HeaderContentTypeJSON)
	}
	req.Header.Set(constants.HeaderXLast9APIToken, constants.BearerPrefix+cfg.TokenManager.GetAccessToken(ctx))

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("alert rule request failed: %w", err)
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, maxAlertMutationResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("failed to read alert rule response: %w", err)
	}
	if int64(len(responseBody)) > maxAlertMutationResponseBytes {
		return nil, fmt.Errorf("alert rule response exceeds %d bytes", maxAlertMutationResponseBytes)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		message := strings.TrimSpace(string(responseBody))
		if len(message) > 4096 {
			message = message[:4096] + "..."
		}
		if message == "" {
			message = http.StatusText(resp.StatusCode)
		}
		return nil, fmt.Errorf("alert rule API returned status %d: %s", resp.StatusCode, message)
	}
	return responseBody, nil
}

func alertMutationResult(cfg models.Config, body []byte, fallback string) *mcp.CallToolResult {
	text := strings.TrimSpace(string(body))
	if text == "" {
		text = fallback
	}
	link := deeplink.NewBuilder(cfg.OrgSlug, cfg.ClusterID).BuildAlertingGroupsLink()
	return &mcp.CallToolResult{Meta: deeplink.ToMeta(link), Content: []mcp.Content{&mcp.TextContent{Text: text}}}
}
