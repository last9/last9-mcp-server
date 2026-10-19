package alerting

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"last9-mcp/internal/last9api"
	"last9-mcp/internal/last9api/entities"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// requireRuleInGroup returns the stored rule, or a tool error unless ruleID exists and
// belongs to groupID. The API acts on the rule id alone, so without this a mismatched id
// pair would write to (or claim) a rule in another alert group.
func requireRuleInGroup(ctx context.Context, api *last9api.Client, groupID, ruleID string) (entities.AlertRule, *mcp.CallToolResult, error) {
	rule, err := entities.GetAlertRule(ctx, api, entities.AlertRuleInput{EntityID: groupID, RuleID: ruleID})
	if err != nil {
		var apiErr *utils.UpstreamHTTPError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			return rule, utils.ToolErrorResult(fmt.Sprintf("no alert rule %s exists (it may have been deleted or replaced by update_alert); nothing was changed. Look up current rule ids with get_alert_config", ruleID)), nil
		}
		return rule, nil, withUpstreamBody(err)
	}
	owner := strings.TrimSpace(rule.EntityID)
	if owner == "" {
		return rule, utils.ToolErrorResult(fmt.Sprintf("could not confirm which alert group owns rule %s; nothing was changed", ruleID)), nil
	}
	if !strings.EqualFold(owner, strings.TrimSpace(groupID)) {
		return rule, utils.ToolErrorResult(fmt.Sprintf("alert rule %s belongs to alert group %s, not %s; nothing was changed. Re-check both ids with get_alert_config", ruleID, owner, groupID)), nil
	}
	return rule, nil, nil
}
