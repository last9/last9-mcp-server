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

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// AlertRule represents an alert configuration from Last9 API
type AlertRule struct {
	ID                           string                 `json:"id"`
	OrganizationID               string                 `json:"organization_id"`
	EntityID                     string                 `json:"entity_id"`
	PrimaryIndicator             string                 `json:"primary_indicator"`
	CreatedAt                    int64                  `json:"created_at"`
	UpdatedAt                    int64                  `json:"updated_at"`
	DeletedAt                    *int64                 `json:"deleted_at"`
	ErrorSince                   *int64                 `json:"error_since"`
	State                        string                 `json:"state"`
	ExternalRef                  string                 `json:"external_ref"`
	Severity                     string                 `json:"severity"`
	Algorithm                    string                 `json:"algorithm"`
	RuleName                     string                 `json:"rule_name"`
	MuteUntil                    int64                  `json:"mute_until"`
	Properties                   map[string]interface{} `json:"properties"`
	GroupTimeseriesNotifications bool                   `json:"group_timeseries_notifications"`
}

// Alert represents an active alert instance
type Alert struct {
	ID           string            `json:"id"`
	RuleID       string            `json:"rule_id"`
	RuleName     string            `json:"rule_name"`
	State        string            `json:"state"`
	Severity     string            `json:"severity"`
	StartsAt     string            `json:"starts_at"`
	EndsAt       string            `json:"ends_at"`
	Labels       map[string]string `json:"labels"`
	Annotations  map[string]string `json:"annotations"`
	GeneratorURL string            `json:"generator_url"`
	Fingerprint  string            `json:"fingerprint"`
}

// AlertConfigResponse represents the response from alert rules API (direct array)
type AlertConfigResponse []AlertRule

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
	RuleProperties map[string]interface{} `json:"rule_properties"`
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

const GetAlertConfigDescription = `
	Get alert configurations (alert rules) from Last9.
	Returns all configured alert rules including their conditions, labels, and annotations.
	Uses the datasource configured in the server config (or default if not specified).
	
	Each alert rule includes:
	- id: Unique identifier for the alert rule
	- name: Human-readable name of the alert
	- description: Detailed description of what the alert monitors
	- state: Current state of the alert rule (active, inactive, etc.)
	- severity: Alert severity level (critical, warning, info)
	- query: PromQL query used for the alert condition
	- for: Duration threshold before alert fires
	- labels: Key-value pairs for alert routing and grouping
	- annotations: Additional metadata and descriptions
	- group_name: Alert group this rule belongs to
	- condition: Alert condition configuration (thresholds, operators)
	- created_at: When the alert rule was created
	- updated_at: When the alert rule was last modified
`

const GetAlertsDescription = `
	Get currently active alerts from Last9 monitoring system.
	Returns all alerts that are currently firing or have fired recently within the specified time window.
	Parameters:
	- timestamp: Unix timestamp for the query time (defaults to current time)
	- window: Time window in seconds to look back for alerts (defaults to 900 seconds = 15 minutes)
	
	Uses the datasource configured in the server config (or default if not specified).
	
	Each alert includes:
	- id: Unique identifier for this alert instance
	- rule_id: ID of the alert rule that triggered this alert
	- rule_name: Name of the alert rule
	- state: Current state (firing, resolved, pending)
	- severity: Alert severity level
	- starts_at: When this alert instance started firing
	- ends_at: When this alert instance was resolved (if resolved)
	- labels: Key-value pairs for alert identification and routing
	- annotations: Additional context and descriptions
	- generator_url: URL to the source of the alert
	- fingerprint: Unique fingerprint for this alert instance
`

type GetAlertConfigArgs struct{}

func NewGetAlertConfigHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, GetAlertConfigArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args GetAlertConfigArgs) (*mcp.CallToolResult, any, error) {
		// Build the URL for alert rules API
		// Datasource is already configured in cfg via PopulateAPICfg
		// If a specific datasource was set, it's already in cfg.PrometheusReadURL
		baseURL := fmt.Sprintf("%s%s", cfg.APIBaseURL, constants.EndpointAlertRules)
		finalURL := baseURL

		// Create HTTP request
		httpReq, err := http.NewRequestWithContext(ctx, "GET", finalURL, nil)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create request: %w", err)
		}

		// Set headers
		httpReq.Header.Set(constants.HeaderAccept, constants.HeaderAcceptJSON)
		httpReq.Header.Set(constants.HeaderXLast9APIToken, constants.BearerPrefix+cfg.TokenManager.GetAccessToken(ctx))

		// Make the request
		resp, err := client.Do(httpReq)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to make request: %w", err)
		}
		defer resp.Body.Close()

		// Check response status
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return nil, nil, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
		}

		// Read response body
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to read response: %w", err)
		}

		// Parse JSON response
		var alertConfig AlertConfigResponse
		if err := json.Unmarshal(body, &alertConfig); err != nil {
			return nil, nil, fmt.Errorf("failed to parse response: %w", err)
		}

		// Format the response
		formattedResponse := fmt.Sprintf("Found %d alert rules:\n\n", len(alertConfig))

		for i, rule := range alertConfig {
			formattedResponse += fmt.Sprintf("Alert Rule %d:\n", i+1)
			formattedResponse += fmt.Sprintf("  ID: %s\n", rule.ID)
			formattedResponse += fmt.Sprintf("  Rule Name: %s\n", rule.RuleName)
			formattedResponse += fmt.Sprintf("  Primary Indicator: %s\n", rule.PrimaryIndicator)
			formattedResponse += fmt.Sprintf("  State: %s\n", rule.State)
			formattedResponse += fmt.Sprintf("  Severity: %s\n", rule.Severity)
			formattedResponse += fmt.Sprintf("  Algorithm: %s\n", rule.Algorithm)
			formattedResponse += fmt.Sprintf("  Entity ID: %s\n", rule.EntityID)

			if rule.ErrorSince != nil {
				errorTime := time.Unix(*rule.ErrorSince, 0).UTC().Format("2006-01-02 15:04:05 UTC")
				formattedResponse += fmt.Sprintf("  Error Since: %s\n", errorTime)
			}

			if len(rule.Properties) > 0 {
				formattedResponse += "  Properties:\n"
				for k, v := range rule.Properties {
					formattedResponse += fmt.Sprintf("    %s: %v\n", k, v)
				}
			}

			createdTime := time.Unix(rule.CreatedAt, 0).UTC().Format("2006-01-02 15:04:05 UTC")
			updatedTime := time.Unix(rule.UpdatedAt, 0).UTC().Format("2006-01-02 15:04:05 UTC")
			formattedResponse += fmt.Sprintf("  Created: %s\n", createdTime)
			formattedResponse += fmt.Sprintf("  Updated: %s\n", updatedTime)
			formattedResponse += fmt.Sprintf("  Group Timeseries Notifications: %v\n", rule.GroupTimeseriesNotifications)
			formattedResponse += "\n"
		}

		// Build deep link URL to alerting groups page
		dlBuilder := deeplink.NewBuilder(cfg.OrgSlug, cfg.ClusterID)
		dashboardURL := dlBuilder.BuildAlertingGroupsLink()

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

type GetAlertsArgs struct {
	Timestamp float64 `json:"timestamp,omitempty" jsonschema:"Unix timestamp for query time (defaults to current time)"`
	Window    float64 `json:"window,omitempty" jsonschema:"Time window in seconds (default: 900, range: 60-86400)"`
}

func NewGetAlertsHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, GetAlertsArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args GetAlertsArgs) (*mcp.CallToolResult, any, error) {
		// Parse timestamp parameter (defaults to current time)
		timestamp := time.Now().Unix()
		if args.Timestamp != 0 {
			timestamp = int64(args.Timestamp)
		}

		// Parse window parameter (defaults to 900 seconds = 15 minutes)
		window := int64(900)
		if args.Window != 0 {
			window = int64(args.Window)
		}

		// Build the base URL for alerts monitoring API
		// Datasource is already configured in cfg via PopulateAPICfg
		baseURL := fmt.Sprintf("%s%s", cfg.APIBaseURL, constants.EndpointAlertsMonitor)
		queryParams := url.Values{}
		queryParams.Set("timestamp", fmt.Sprintf("%d", timestamp))
		queryParams.Set("window", fmt.Sprintf("%d", window))
		// Note: read_url is not needed here as datasource is configured at config level

		// Build final URL with query parameters
		finalURL := fmt.Sprintf("%s?%s", baseURL, queryParams.Encode())

		// Create HTTP request
		httpReq, err := http.NewRequestWithContext(ctx, "GET", finalURL, nil)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create request: %w", err)
		}

		// Set headers
		httpReq.Header.Set(constants.HeaderAccept, constants.HeaderAcceptJSON)
		httpReq.Header.Set(constants.HeaderXLast9APIToken, constants.BearerPrefix+cfg.TokenManager.GetAccessToken(ctx))

		// Make the request
		resp, err := client.Do(httpReq)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to make request: %w", err)
		}
		defer resp.Body.Close()

		// Check response status
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return nil, nil, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
		}

		// Read response body
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to read response: %w", err)
		}

		// Parse JSON response
		var alertsResp AlertsResponse
		if err := json.Unmarshal(body, &alertsResp); err != nil {
			return nil, nil, fmt.Errorf("failed to parse response: %w", err)
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
