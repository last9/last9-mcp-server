package alerting

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"last9-mcp/internal/constants"
	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const GetAlertRuleStateDescription = `Gets the historical firing state of alert rules over a specified time range, grouped by rule_id.
It polls the /alerts/monitor API at each step within the time range and returns 1 for firing and 0 otherwise at each timestamp.

Required parameters:
- start_time: Unix epoch start of the range (inclusive)
- end_time: Unix epoch end of the range (inclusive)
- step: Resolution in seconds between samples

Optional filters forwarded to the upstream API (no client-side filtering is applied):
- alert_group_id: Filter by alert group ID
- rule_name: Regex filter on rule name
- alert_group_name: Regex filter on alert group name
- label_filters: Comma-separated key=value label filters
- state: Filter by state (e.g. firing)

Output is a JSON map of rule_id -> [{timestamp, is_firing}]. A timestamp at which a rule is absent from the upstream
response is reported as is_firing=0; this reflects "not observed as firing" and not necessarily a confirmed normal state.
The number of samples ((end_time - start_time) / step + 1) is capped at 100.`

type AlertRuleStateRequest struct {
	AlertGroupID   string `json:"alert_group_id,omitempty" jsonschema:"Optional filter by alert group ID"`
	RuleName       string `json:"rule_name,omitempty" jsonschema:"Optional regex filter by rule name"`
	AlertGroupName string `json:"alert_group_name,omitempty" jsonschema:"Optional regex filter by alert group name"`
	LabelFilters   string `json:"label_filters,omitempty" jsonschema:"Optional comma separated key-value label filters"`
	State          string `json:"state,omitempty" jsonschema:"Optional state filter (e.g. firing)"`
	StartTime      int64  `json:"start_time" jsonschema:"Start time in unix epoch (required)"`
	EndTime        int64  `json:"end_time" jsonschema:"End time in unix epoch (required)"`
	Step           int64  `json:"step" jsonschema:"Resolution step in seconds (required)"`
}

const alertRuleStateMaxPoints = 100

func toolErrorResult(msg string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: msg}},
		IsError: true,
	}
}

func NewAlertRuleStateHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, AlertRuleStateRequest) (*mcp.CallToolResult, any, error) {
	if client == nil {
		client = http.DefaultClient
	}
	return func(ctx context.Context, req *mcp.CallToolRequest, args AlertRuleStateRequest) (*mcp.CallToolResult, any, error) {
		if args.StartTime >= args.EndTime {
			return toolErrorResult("start_time must be less than end_time"), nil, nil
		}
		if args.Step <= 0 {
			return toolErrorResult("step must be greater than 0"), nil, nil
		}

		// Inclusive sample count: t iterates start, start+step, ..., end.
		points := (args.EndTime-args.StartTime)/args.Step + 1
		if points > alertRuleStateMaxPoints {
			return toolErrorResult(fmt.Sprintf("time range and step result in too many points (%d). Maximum is %d", points, alertRuleStateMaxPoints)), nil, nil
		}

		type Datapoint struct {
			Timestamp int64 `json:"timestamp"`
			IsFiring  int   `json:"is_firing"`
		}

		// results is a map of rule_id -> timestamp -> is_firing
		results := make(map[string]map[int64]int)

		tokenMgr := cfg.TokenManager

		for t := args.StartTime; t <= args.EndTime; t += args.Step {
			queryParams := url.Values{}
			queryParams.Set("timestamp", fmt.Sprintf("%d", t))
			queryParams.Set("window", fmt.Sprintf("%d", args.Step))

			if args.AlertGroupID != "" {
				queryParams.Set("alert_group_id", args.AlertGroupID)
			}
			if args.RuleName != "" {
				queryParams.Set("rule_name", args.RuleName)
			}
			if args.AlertGroupName != "" {
				queryParams.Set("alert_group_name", args.AlertGroupName)
			}
			if args.LabelFilters != "" {
				queryParams.Set("label_filters", args.LabelFilters)
			}
			if args.State != "" {
				queryParams.Set("state", args.State)
			}

			finalURL := fmt.Sprintf("%s%s?%s", cfg.APIBaseURL, constants.EndpointAlertsMonitor, queryParams.Encode())

			httpReq, err := http.NewRequestWithContext(ctx, "GET", finalURL, nil)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to create request for timestamp %d: %w", t, err)
			}

			httpReq.Header.Set(constants.HeaderAccept, constants.HeaderAcceptJSON)
			if tokenMgr != nil {
				// Fetch per-iteration so token-manager refresh logic stays in effect for long ranges.
				if token := tokenMgr.GetAccessToken(ctx); token != "" {
					httpReq.Header.Set(constants.HeaderXLast9APIToken, constants.BearerPrefix+token)
				}
			}

			resp, err := client.Do(httpReq)
			if err != nil {
				return nil, nil, fmt.Errorf("API request failed at timestamp %d: %w", t, err)
			}

			body, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil {
				return nil, nil, fmt.Errorf("failed to read response body at timestamp %d: %w", t, err)
			}

			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				return nil, nil, fmt.Errorf("API returned error status %d at timestamp %d: %s", resp.StatusCode, t, string(body))
			}

			var alertResp AlertsResponse
			if err := json.Unmarshal(body, &alertResp); err != nil {
				return nil, nil, fmt.Errorf("failed to unmarshal alerts response at timestamp %d: %w", t, err)
			}

			for _, rule := range alertResp.AlertRules {
				if _, exists := results[rule.RuleID]; !exists {
					results[rule.RuleID] = make(map[int64]int)
				}
				if rule.State == "firing" {
					results[rule.RuleID][t] = 1
				} else {
					results[rule.RuleID][t] = 0
				}
			}
		}

		// Construct final continuous timeseries for all seen rules. Gaps are zero-filled; see tool description for caveats.
		finalResults := make(map[string][]Datapoint)
		for ruleID, tsMap := range results {
			var dps []Datapoint
			for t := args.StartTime; t <= args.EndTime; t += args.Step {
				dps = append(dps, Datapoint{
					Timestamp: t,
					IsFiring:  tsMap[t],
				})
			}
			finalResults[ruleID] = dps
		}

		formattedBytes, err := json.MarshalIndent(finalResults, "", "  ")
		if err != nil {
			return nil, nil, fmt.Errorf("failed to marshal results: %w", err)
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: string(formattedBytes),
				},
			},
		}, nil, nil
	}
}
