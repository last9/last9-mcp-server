package alerting

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"last9-mcp/internal/deeplink"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

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

func unreadableStoredPropertiesResult(ruleID string, err error) *mcp.CallToolResult {
	return utils.ToolErrorResult(fmt.Sprintf("could not read the stored properties of alert rule %s (%v); nothing was changed", ruleID, err))
}

func alertMutationResult(cfg models.Config, body []byte, fallback string) *mcp.CallToolResult {
	text := strings.TrimSpace(string(body))
	if text == "" {
		text = fallback
	}
	link := deeplink.NewBuilder(cfg.OrgSlug, cfg.ClusterID).BuildAlertingGroupsLink()
	return &mcp.CallToolResult{Meta: deeplink.ToMeta(link), Content: []mcp.Content{&mcp.TextContent{Text: text}}}
}

// staticRuleRequiredFields maps the API's errors for a static rule missing a field to that
// field. The API decides whether a rule is static, so the tool maps its answer rather than
// re-deriving the algorithm from the expression.
var staticRuleRequiredFields = map[string]string{
	"condition required":       "alert_rule.condition",
	"alert condition required": "alert_rule.alert_condition",
	"eval window required":     "alert_rule.eval_window",
}

// alertRuleWriteError turns create/update failures into results. The API answers a key that
// is not the bound KPI's own name and a KPI of another group with the same bare
// "Invalid expression_args", so the hint names both and how to tell them apart.
func alertRuleWriteError(err error) (*mcp.CallToolResult, any, error) {
	var apiErr *utils.UpstreamHTTPError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusBadRequest {
		return nil, nil, withUpstreamBody(err)
	}
	if strings.Contains(apiErr.Body, "Invalid expression_args") {
		return utils.ToolErrorResult("the API rejected expression_args; nothing was saved. Either a KPI id is not a KPI of entity_id, or a key is not the exact name of the KPI it binds (not a placeholder such as x). To tell which, run validate_alert_expression with the same bindings: \"invalid indicator details for <name>\" means that KPI belongs to another alert group. Otherwise use each KPI's exact name (existing rules show them under Indicators in get_entity_alert_rules) as its key, in primary_indicator and in the expression."), nil, nil
	}
	var body struct {
		Error string `json:"error"`
	}
	if json.Unmarshal([]byte(apiErr.Body), &body) == nil {
		if field, ok := staticRuleRequiredFields[body.Error]; ok {
			return utils.ToolErrorResult(field + " is required for a static rule (an expression without adaptive_std_cmp); nothing was saved. Static rules need condition, alert_condition and eval_window."), nil, nil
		}
	}
	return nil, nil, withUpstreamBody(err)
}

// withUpstreamBody relays a sanitized upstream 4xx body that the error text omits (403/404/409).
func withUpstreamBody(err error) error {
	var apiErr *utils.UpstreamHTTPError
	if errors.As(err, &apiErr) && apiErr.Body != "" && !strings.Contains(err.Error(), apiErr.Body) {
		sep := ". "
		if strings.HasSuffix(err.Error(), ".") {
			sep = " "
		}
		return fmt.Errorf("%w%sUpstream response: %s", err, sep, apiErr.Body)
	}
	return err
}
