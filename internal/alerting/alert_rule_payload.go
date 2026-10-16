package alerting

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"last9-mcp/internal/deeplink"
	"last9-mcp/internal/last9api"
	"last9-mcp/internal/last9api/entities"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	severityBreach = "breach"
	severityThreat = "threat"
)

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
	Expression                   *string                       `json:"expression,omitempty" jsonschema:"Rule expression. Plain arithmetic over indicators (static) or adaptive_std_cmp(<primary_indicator>, sensitivity, window) (adaptive); clients cannot set algorithm directly."`
	Condition                    *string                       `json:"condition,omitempty" jsonschema:"Required for static rules; boolean expression over expr, for example expr > 100"`
	AlertCondition               *string                       `json:"alert_condition,omitempty" jsonschema:"Required for static rules; firing window expression, for example count_true(result) >= 3"`
	EvalWindow                   *int                          `json:"eval_window,omitempty" jsonschema:"Required for static rules; evaluation window in minutes, range 0-60"`
	IsDisabled                   *bool                         `json:"is_disabled,omitempty" jsonschema:"Ignored by create_alert, which always creates the rule disabled; promote with patch_alert"`
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
	if rule.Severity != nil && *rule.Severity != severityBreach && *rule.Severity != severityThreat {
		return fmt.Errorf("alert_rule.severity must be breach or threat")
	}
	if rule.Properties != nil && rule.Properties.QueryMode != "" && rule.Properties.QueryMode != "builder" && rule.Properties.QueryMode != "code" {
		return fmt.Errorf("alert_rule.properties.query_mode must be builder or code")
	}
	return nil
}

// requireRuleInGroup returns a tool error unless ruleID exists and belongs to groupID.
// The API acts on the rule id alone, so without this a mismatched id pair would write to
// (or claim) a rule in another alert group.
func requireRuleInGroup(ctx context.Context, api *last9api.Client, groupID, ruleID string) (*mcp.CallToolResult, error) {
	rule, err := entities.GetAlertRule(ctx, api, entities.AlertRuleInput{EntityID: groupID, RuleID: ruleID})
	if err != nil {
		var apiErr *utils.UpstreamHTTPError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			return utils.ToolErrorResult(fmt.Sprintf("no alert rule %s exists (it may have been deleted or replaced by update_alert); nothing was changed. Look up current rule ids with get_alert_config", ruleID)), nil
		}
		return nil, withUpstreamBody(err)
	}
	owner := strings.TrimSpace(rule.EntityID)
	if owner == "" {
		return utils.ToolErrorResult(fmt.Sprintf("could not confirm which alert group owns rule %s; nothing was changed", ruleID)), nil
	}
	if !strings.EqualFold(owner, strings.TrimSpace(groupID)) {
		return utils.ToolErrorResult(fmt.Sprintf("alert rule %s belongs to alert group %s, not %s; nothing was changed. Re-check both ids with get_alert_config", ruleID, owner, groupID)), nil
	}
	return nil, nil
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

func alertMutationResult(cfg models.Config, body []byte, fallback string) *mcp.CallToolResult {
	text := strings.TrimSpace(string(body))
	if text == "" {
		text = fallback
	}
	link := deeplink.NewBuilder(cfg.OrgSlug, cfg.ClusterID).BuildAlertingGroupsLink()
	return &mcp.CallToolResult{Meta: deeplink.ToMeta(link), Content: []mcp.Content{&mcp.TextContent{Text: text}}}
}

// withUpstreamBody relays a sanitized upstream 4xx body that the error text omits (403/404/409).
func withUpstreamBody(err error) error {
	var apiErr *utils.UpstreamHTTPError
	if errors.As(err, &apiErr) && apiErr.Body != "" && !strings.Contains(err.Error(), apiErr.Body) {
		return fmt.Errorf("%w. Upstream response: %s", err, apiErr.Body)
	}
	return err
}
