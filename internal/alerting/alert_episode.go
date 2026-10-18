package alerting

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"last9-mcp/internal/constants"
	"last9-mcp/internal/last9api"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// alertEpisode names one alert as get_alerts lists it.
type alertEpisode struct {
	AlertGroupID, RuleID, RuleType string
}

// ackLookbackSeconds is the widest window the alert monitor serves (get_alerts' maximum).
const ackLookbackSeconds = 3600

// requireRecentAlert returns a tool error unless get_alerts' source lists this exact alert
// in the last hour. The claim API records a claim for any id triple, and episodes may come
// from scheduled searches as well as alert rules, so the monitor is the one place to check.
func requireRecentAlert(ctx context.Context, api *last9api.Client, episode alertEpisode) (*mcp.CallToolResult, error) {
	var monitor AlertsResponse
	query := url.Values{}
	query.Set("timestamp", strconv.FormatInt(time.Now().Unix(), 10))
	query.Set("window", strconv.Itoa(ackLookbackSeconds))
	err := api.Do(ctx, "list recent alerts", last9api.Request{Method: http.MethodGet, Path: constants.EndpointAlertsMonitor}, &monitor, last9api.WithQuery(query))
	if err != nil {
		return nil, withUpstreamBody(err)
	}
	for _, rule := range monitor.AlertRules {
		if strings.EqualFold(rule.AlertGroupID, strings.TrimSpace(episode.AlertGroupID)) &&
			strings.EqualFold(rule.RuleID, strings.TrimSpace(episode.RuleID)) &&
			strings.EqualFold(rule.RuleType, strings.TrimSpace(episode.RuleType)) {
			return nil, nil
		}
	}
	return utils.ToolErrorResult("no alert with this alert_group_id, rule_id and rule_type fired in the last hour; nothing was changed. Copy all three from the alert in get_alerts"), nil
}
