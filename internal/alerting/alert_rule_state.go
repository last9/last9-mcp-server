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

const GetAlertRuleStateDescription = `Gets the historical firing state of an alert rule over a specified time range.
It returns 1 for firing and 0 for not firing at each timestamp.
Requires start_time (Unix timestamp), end_time (Unix timestamp), and step (resolution in seconds).`

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

func NewAlertRuleStateHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, AlertRuleStateRequest) (*mcp.CallToolResult, any, error) {
	if client == nil {
		client = http.DefaultClient
	}
	return func(ctx context.Context, req *mcp.CallToolRequest, args AlertRuleStateRequest) (*mcp.CallToolResult, any, error) {
		if args.StartTime >= args.EndTime {
			return nil, nil, fmt.Errorf("start_time must be less than end_time")
		}
		if args.Step <= 0 {
			return nil, nil, fmt.Errorf("step must be greater than 0")
		}

		// Cap maximum points to prevent excessive API calls
		points := (args.EndTime - args.StartTime) / args.Step
		if points > 100 {
			return nil, nil, fmt.Errorf("time range and step result in too many points (%d). Maximum is 100", points)
		}

		type Datapoint struct {
			Timestamp int64 `json:"timestamp"`
			IsFiring  int   `json:"is_firing"`
		}

		// results is a map of rule_id -> timestamp -> is_firing
		results := make(map[string]map[int64]int)

		tokenMgr := cfg.TokenManager
		var token string
		if tokenMgr != nil {
			token = tokenMgr.GetAccessToken(ctx)
		}

		for t := args.StartTime; t <= args.EndTime; t += args.Step {
			// Construct query parameters
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

			// Query the /alerts/monitor API for each timestamp
			url := fmt.Sprintf("%s/alerts/monitor?%s", cfg.APIBaseURL, queryParams.Encode())
			
			httpReq, err := http.NewRequestWithContext(ctx, "GET", url, nil)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to create request for timestamp %d: %w", t, err)
			}

			httpReq.Header.Set(constants.HeaderAccept, constants.HeaderAcceptJSON)
			if token != "" {
				httpReq.Header.Set(constants.HeaderXLast9APIToken, constants.BearerPrefix+token)
				httpReq.Header.Set("Authorization", constants.BearerPrefix+token)
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

			// Parse the alerts response
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
					results[rule.RuleID][t] = 0 // Some rules might explicitly be "normal"/"pending"
				}
			}
		}

		// Construct final continuous timeseries for all seen rules
		finalResults := make(map[string][]Datapoint)
		for ruleID, tsMap := range results {
			var dps []Datapoint
			for t := args.StartTime; t <= args.EndTime; t += args.Step {
				isFiring := tsMap[t] // defaults to 0 if no record for that timestamp
				dps = append(dps, Datapoint{
					Timestamp: t,
					IsFiring:  isFiring,
				})
			}
			finalResults[ruleID] = dps
		}

		formattedBytes, _ := json.MarshalIndent(finalResults, "", "  ")

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: string(formattedBytes),
				},
			},
		}, nil, nil
	}
}
