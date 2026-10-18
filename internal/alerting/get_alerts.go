package alerting

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"last9-mcp/internal/constants"
	"last9-mcp/internal/deeplink"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// AlertsResponse represents the response from alerts monitoring API
type AlertsResponse struct {
	Timestamp  int64           `json:"timestamp"`
	Window     int64           `json:"window"`
	AlertRules []AlertRuleData `json:"alert_rules"`
}

type AlertRuleData struct {
	AlertGroupID   string                 `json:"alert_group_id"`
	AlertGroupName string                 `json:"alert_group_name"`
	RuleID         string                 `json:"rule_id"`
	RuleName       string                 `json:"rule_name"`
	State          string                 `json:"state"`
	Severity       string                 `json:"severity"`
	RuleType       string                 `json:"rule_type"`
	LastFiredAt    int64                  `json:"last_fired_at"`
	Since          int64                  `json:"since"`
	Alerts         []AlertInstance        `json:"alerts"`
	Claim          *AlertClaim            `json:"claim,omitempty"`
	RuleProperties map[string]interface{} `json:"rule_properties"`
}

type AlertClaim struct {
	OwnerUserID     string `json:"owner_user_id"`
	OwnerName       string `json:"owner_name,omitempty"`
	OwnerEmail      string `json:"owner_email,omitempty"`
	ClaimedByUserID string `json:"claimed_by_user_id"`
	ClaimedAt       int64  `json:"claimed_at"`
	MutedUntil      int64  `json:"muted_until"`
}

type AlertInstance struct {
	State             string                 `json:"state"`
	LabelHash         string                 `json:"label_hash"`
	Annotations       map[string]interface{} `json:"annotations"`
	GroupLabels       map[string]interface{} `json:"group_labels"`
	MetricDegradation float64                `json:"metric_degradation"`
	CurrentValue      float64                `json:"current_value"`
	LastFiredAt       int64                  `json:"last_fired_at"`
	Since             int64                  `json:"since"`
}

// MaxAlertsMonitorWindowSeconds is the widest window one alerts-monitor call accepts.
const MaxAlertsMonitorWindowSeconds = 3600

// AlertsMonitorQuery asks for the alert rules active in the Window seconds
// ending at Timestamp (unix seconds).
type AlertsMonitorQuery struct {
	Timestamp int64
	Window    int64
}

// FetchAlertsMonitor calls GET /alerts/monitor. The datasource comes from cfg.
func FetchAlertsMonitor(ctx context.Context, client *http.Client, cfg models.Config, q AlertsMonitorQuery) (AlertsResponse, error) {
	queryParams := url.Values{}
	queryParams.Set("timestamp", fmt.Sprintf("%d", q.Timestamp))
	queryParams.Set("window", fmt.Sprintf("%d", q.Window))
	finalURL := fmt.Sprintf("%s%s?%s", cfg.APIBaseURL, constants.EndpointAlertsMonitor, queryParams.Encode())

	httpReq, err := http.NewRequestWithContext(ctx, "GET", finalURL, nil)
	if err != nil {
		return AlertsResponse{}, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set(constants.HeaderAccept, constants.HeaderAcceptJSON)
	httpReq.Header.Set(constants.HeaderXLast9APIToken, constants.BearerPrefix+cfg.TokenManager.GetAccessToken(ctx))

	resp, err := client.Do(httpReq)
	if err != nil {
		return AlertsResponse{}, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return AlertsResponse{}, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return AlertsResponse{}, fmt.Errorf("failed to read response: %w", err)
	}
	var alertsResp AlertsResponse
	if err := json.Unmarshal(body, &alertsResp); err != nil {
		return AlertsResponse{}, fmt.Errorf("failed to parse response: %w", err)
	}
	return alertsResp, nil
}

type GetAlertsArgs struct {
	TimeISO         string  `json:"time_iso,omitempty" jsonschema:"Evaluation time in RFC3339/ISO8601 format (e.g. 2026-02-09T15:04:05Z)"`
	Timestamp       float64 `json:"timestamp,omitempty" jsonschema:"Unix timestamp for query time (deprecated alias; defaults to current time)"`
	Window          float64 `json:"window,omitempty" jsonschema:"Time window in seconds (default: 900, range: 1-3600)"`
	LookbackMinutes float64 `json:"lookback_minutes,omitempty" jsonschema:"Time window in minutes (default: 15, range: 1-60). Used only when window is omitted."`
}

func NewGetAlertsHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, GetAlertsArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args GetAlertsArgs) (*mcp.CallToolResult, any, error) {
		// Parse window parameter (defaults to 900 seconds = 15 minutes).
		window := int64(900)
		if args.Window != 0 {
			window = int64(args.Window)
		} else if args.LookbackMinutes != 0 {
			if args.LookbackMinutes < 1 || args.LookbackMinutes > 60 {
				return nil, nil, fmt.Errorf("lookback_minutes must be between 1 and 60")
			}
			window = int64(args.LookbackMinutes * 60)
		}

		if window < 1 || window > MaxAlertsMonitorWindowSeconds {
			return nil, nil, fmt.Errorf("window must be between 1 and %d seconds", MaxAlertsMonitorWindowSeconds)
		}

		// Resolve timestamp using shared time-range logic.
		timeParams := map[string]interface{}{}
		if args.TimeISO != "" {
			timeParams["end_time_iso"] = args.TimeISO
		} else if args.Timestamp != 0 {
			timeParams["end_time_iso"] = time.Unix(int64(args.Timestamp), 0).UTC().Format(time.RFC3339)
		} else if args.LookbackMinutes != 0 && args.Window == 0 {
			timeParams["lookback_minutes"] = args.LookbackMinutes
		}

		defaultLookbackMinutes := int(window / 60)
		if defaultLookbackMinutes < 1 {
			defaultLookbackMinutes = 1
		}

		_, endTime, err := utils.GetTimeRange(timeParams, defaultLookbackMinutes)
		if err != nil {
			if args.TimeISO != "" {
				return nil, nil, fmt.Errorf("invalid time_iso format: %w", err)
			}
			return nil, nil, err
		}
		timestamp := endTime.Unix()

		alertsResp, err := FetchAlertsMonitor(ctx, client, cfg, AlertsMonitorQuery{Timestamp: timestamp, Window: window})
		if err != nil {
			return nil, nil, err
		}

		// Format the response
		timeStr := time.Unix(alertsResp.Timestamp, 0).UTC().Format("2006-01-02 15:04:05 UTC")
		formattedResponse := fmt.Sprintf("Alerts for timestamp %s (window: %d seconds):\n", timeStr, alertsResp.Window)

		totalAlertInstances := 0
		for _, rule := range alertsResp.AlertRules {
			totalAlertInstances += len(rule.Alerts)
		}

		formattedResponse += fmt.Sprintf("Found %d alert rule(s) with %d alert instance(s):\n\n", len(alertsResp.AlertRules), totalAlertInstances)

		if len(alertsResp.AlertRules) == 0 {
			formattedResponse += "No alerts found in the specified time window.\n"
		} else {
			for i, rule := range alertsResp.AlertRules {
				formattedResponse += fmt.Sprintf("Alert Rule %d:\n", i+1)
				formattedResponse += fmt.Sprintf("  Rule ID: %s\n", rule.RuleID)
				formattedResponse += fmt.Sprintf("  Rule Name: %s\n", rule.RuleName)
				formattedResponse += fmt.Sprintf("  Alert Group: %s\n", rule.AlertGroupName)
				formattedResponse += fmt.Sprintf("  Alert Group ID: %s\n", rule.AlertGroupID)
				formattedResponse += fmt.Sprintf("  State: %s\n", rule.State)
				formattedResponse += fmt.Sprintf("  Severity: %s\n", rule.Severity)
				formattedResponse += fmt.Sprintf("  Rule Type: %s\n", rule.RuleType)

				if rule.LastFiredAt > 0 {
					lastFired := time.Unix(rule.LastFiredAt, 0).UTC().Format("2006-01-02 15:04:05 UTC")
					formattedResponse += fmt.Sprintf("  Last Fired At: %s\n", lastFired)
				}

				if rule.Since > 0 {
					since := time.Unix(rule.Since, 0).UTC().Format("2006-01-02 15:04:05 UTC")
					formattedResponse += fmt.Sprintf("  Since: %s\n", since)
				}

				if rule.Claim != nil {
					formattedResponse += formatAlertClaim(rule.Claim)
				}

				if len(rule.RuleProperties) > 0 {
					formattedResponse += "  Rule Properties:\n"
					for k, v := range rule.RuleProperties {
						formattedResponse += fmt.Sprintf("    %s: %v\n", k, v)
					}
				}

				if len(rule.Alerts) > 0 {
					formattedResponse += fmt.Sprintf("  Alert Instances (%d):\n", len(rule.Alerts))
					for j, alert := range rule.Alerts {
						formattedResponse += fmt.Sprintf("    Instance %d:\n", j+1)
						formattedResponse += fmt.Sprintf("      State: %s\n", alert.State)
						if alert.LabelHash != "" {
							formattedResponse += fmt.Sprintf("      Label Hash: %s\n", alert.LabelHash)
						}
						formattedResponse += fmt.Sprintf("      Current Value: %.4f\n", alert.CurrentValue)
						formattedResponse += fmt.Sprintf("      Metric Degradation: %.4f\n", alert.MetricDegradation)

						if len(alert.GroupLabels) > 0 {
							formattedResponse += "      Group Labels:\n"
							for k, v := range alert.GroupLabels {
								formattedResponse += fmt.Sprintf("        %s: %v\n", k, v)
							}
						}

						if len(alert.Annotations) > 0 {
							formattedResponse += "      Annotations:\n"
							for k, v := range alert.Annotations {
								formattedResponse += fmt.Sprintf("        %s: %v\n", k, v)
							}
						}
					}
				}

				formattedResponse += "\n"
			}
		}

		// Build deep link URL
		dlBuilder := deeplink.NewBuilder(cfg.OrgSlug, cfg.ClusterID)
		dashboardURL := dlBuilder.BuildAlertingLink((timestamp-window)*1000, timestamp*1000, "", "")

		return &mcp.CallToolResult{
			Meta: deeplink.ToMeta(dashboardURL),
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: formattedResponse,
				},
			},
		}, nil, nil
	}
}

func formatAlertClaim(c *AlertClaim) string {
	const layout = "2006-01-02 15:04:05 UTC"
	out := "  Claim:\n"
	out += fmt.Sprintf("    Owner User ID: %s\n", c.OwnerUserID)
	if c.OwnerName != "" {
		out += fmt.Sprintf("    Owner Name: %s\n", c.OwnerName)
	}
	if c.OwnerEmail != "" {
		out += fmt.Sprintf("    Owner Email: %s\n", c.OwnerEmail)
	}
	out += fmt.Sprintf("    Claimed By User ID: %s\n", c.ClaimedByUserID)
	if c.ClaimedAt > 0 {
		out += fmt.Sprintf("    Claimed At: %s\n", time.Unix(c.ClaimedAt, 0).UTC().Format(layout))
	}
	if c.MutedUntil > 0 {
		out += fmt.Sprintf("    Muted Until: %s\n", time.Unix(c.MutedUntil, 0).UTC().Format(layout))
	}
	return out
}
