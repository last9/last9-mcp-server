package alerting

import (
	"context"

	"fmt"

	"net/http"

	"time"

	"last9-mcp/internal/deeplink"
	"last9-mcp/internal/last9api"
	"last9-mcp/internal/last9api/alerts"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type AlertsResponse = alerts.GetMonitorDetailsResponse
type AlertRuleData = alerts.AlertRuleData
type AlertClaim = alerts.AlertClaim
type AlertInstance = alerts.AlertInstance

// MaxAlertsMonitorWindowSeconds is the widest window one alerts-monitor call accepts.
const MaxAlertsMonitorWindowSeconds = 3600

type AlertsMonitorQuery = alerts.GetMonitorInput

func FetchAlertsMonitor(ctx context.Context, client *http.Client, cfg models.Config, q AlertsMonitorQuery) (AlertsResponse, error) {
	return alerts.GetMonitorDetails(ctx, last9api.NewClient(client, cfg), q)
}

// DefaultAlertsWindow is get_alerts' window when the caller sets none.
const DefaultAlertsWindow = 15 * time.Minute

type GetAlertsArgs struct {
	Limit           *int    `json:"limit,omitempty" jsonschema:"Positive maximum number of alert instances to show across all rules after state filtering. Omit for all matches."`
	State           string  `json:"state,omitempty" jsonschema:"Filter alert instances by firing or normal. For both states omit the state key entirely. Never send null or all."`
	TimeISO         string  `json:"time_iso,omitempty" jsonschema:"Evaluation time in RFC3339/ISO8601 format (e.g. 2026-02-09T15:04:05Z)"`
	Timestamp       float64 `json:"timestamp,omitempty" jsonschema:"Unix timestamp for query time (deprecated alias; defaults to current time)"`
	Window          float64 `json:"window,omitempty" jsonschema:"Time window in seconds (default: 900, range: 1-3600)"`
	LookbackMinutes float64 `json:"lookback_minutes,omitempty" jsonschema:"Time window in minutes (default: 15, range: 1-60). Used only when window is omitted."`
}

func GetAlertsInputSchema() *jsonschema.Schema {
	schema, err := jsonschema.For[GetAlertsArgs](nil)
	if err != nil {
		panic(fmt.Sprintf("get_alerts input schema: %v", err))
	}
	schema.Properties["state"].Enum = []any{"firing", "normal"}
	schema.Properties["limit"].Type = "integer"
	schema.Properties["limit"].Types = nil
	schema.Properties["limit"].Minimum = jsonschema.Ptr(1.0)
	return schema
}

func NewGetAlertsHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, GetAlertsArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args GetAlertsArgs) (*mcp.CallToolResult, any, error) {
		if args.Limit != nil && *args.Limit < 1 {
			return nil, nil, fmt.Errorf("limit must be a positive integer")
		}
		if args.State != "" && args.State != "firing" && args.State != "normal" {
			return nil, nil, fmt.Errorf("state must be firing or normal; omit it to include both")
		}
		window := int64(DefaultAlertsWindow / time.Second)
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

		alertsResp, err := FetchAlertsMonitor(ctx, client, cfg, AlertsMonitorQuery{Timestamp: timestamp, Window: window, State: args.State})
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

		rules := alertsResp.AlertRules
		if args.Limit == nil {
			formattedResponse += fmt.Sprintf("Found %d alert rule(s) with %d alert instance(s):\n\n", len(rules), totalAlertInstances)
		} else {
			rules = firstAlertInstances(rules, *args.Limit)
			formattedResponse += fmt.Sprintf("Showing %d of %d alert instance(s) across %d of %d alert rule(s):\n\n", min(*args.Limit, totalAlertInstances), totalAlertInstances, len(rules), len(alertsResp.AlertRules))
		}

		if len(rules) == 0 {
			if args.State != "" {
				formattedResponse += fmt.Sprintf("No %s alert instances found in the specified time window.\n", args.State)
			} else {
				formattedResponse += "No alerts found in the specified time window.\n"
			}
		} else {
			for i, rule := range rules {
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

func firstAlertInstances(rules []AlertRuleData, limit int) []AlertRuleData {
	var shown []AlertRuleData
	for _, rule := range rules {
		count := min(limit, len(rule.Alerts))
		if count == 0 {
			continue
		}
		rule.Alerts = rule.Alerts[:count]
		shown = append(shown, rule)
		limit -= count
		if limit == 0 {
			break
		}
	}
	return shown
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
