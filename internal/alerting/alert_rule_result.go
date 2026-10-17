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

func alertMutationResult(cfg models.Config, body []byte, fallback string) *mcp.CallToolResult {
	text := strings.TrimSpace(string(body))
	if text == "" {
		text = fallback
	}
	link := deeplink.NewBuilder(cfg.OrgSlug, cfg.ClusterID).BuildAlertingGroupsLink()
	return &mcp.CallToolResult{Meta: deeplink.ToMeta(link), Content: []mcp.Content{&mcp.TextContent{Text: text}}}
}

// alertRuleWriteError turns create/update failures into results. The API accepts an
// expression_args key only when it equals the bound KPI's own name (validate_alert_expression
// does not check this), and its bare "Invalid expression_args" gives no hint of that.
func alertRuleWriteError(err error) (*mcp.CallToolResult, any, error) {
	var apiErr *utils.UpstreamHTTPError
	if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusBadRequest && strings.Contains(apiErr.Body, "Invalid expression_args") {
		return utils.ToolErrorResult("the API rejected expression_args; nothing was saved. Each key must be the exact name of the KPI it binds, not a placeholder such as x (existing rules show KPI names under Indicators in get_entity_alert_rules); primary_indicator and the expression must use those same names, and every KPI id must belong to entity_id."), nil, nil
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
