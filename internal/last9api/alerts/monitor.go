// Package alerts holds the alerting endpoints.
package alerts

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"last9-mcp/internal/last9api"
)

const pathMonitor = "/alerts/monitor"

const opMonitor = "alerts monitor"

type GetMonitorInput struct {
	// Evaluation time, Unix seconds; Window is the lookback in seconds.
	Timestamp, Window int64
	// Optional filters, sent only when non-empty.
	AlertGroupID, RuleName, AlertGroupName, LabelFilters, State string
}

type MonitorAlert struct {
	State string `json:"state"`
}

type MonitorRule struct {
	RuleID string         `json:"rule_id"`
	State  string         `json:"state"`
	Alerts []MonitorAlert `json:"alerts"`
}

type GetMonitorResponse struct {
	AlertRules []MonitorRule `json:"alert_rules"`
}

type GetMonitorDetailsResponse struct {
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

func (in GetMonitorInput) query() url.Values {
	q := url.Values{}
	q.Set("timestamp", strconv.FormatInt(in.Timestamp, 10))
	q.Set("window", strconv.FormatInt(in.Window, 10))
	for key, value := range map[string]string{
		"alert_group_id":   in.AlertGroupID,
		"rule_name":        in.RuleName,
		"alert_group_name": in.AlertGroupName,
		"label_filters":    in.LabelFilters,
		"state":            in.State,
	} {
		if value != "" {
			q.Set(key, value)
		}
	}
	return q
}

// GetMonitorDetails calls GET /alerts/monitor.
func GetMonitorDetails(ctx context.Context, c *last9api.Client, in GetMonitorInput) (GetMonitorDetailsResponse, error) {
	var out GetMonitorDetailsResponse
	err := c.Do(ctx, "alerts", last9api.Request{Method: http.MethodGet, Path: pathMonitor}, &out, last9api.WithQuery(in.query()), last9api.WithRequiredBody(), last9api.WithSuccessStatus(http.StatusOK))
	return out, err
}

// GetMonitor calls GET /alerts/monitor.
func GetMonitor(ctx context.Context, c *last9api.Client, in GetMonitorInput) (GetMonitorResponse, error) {
	// Callers read an absent rule as not firing, so a reply without alert_rules
	// must fail rather than look like a quiet window.
	var wire struct {
		AlertRules *[]MonitorRule `json:"alert_rules"`
	}
	err := c.Do(ctx, opMonitor, last9api.Request{Method: http.MethodGet, Path: pathMonitor}, &wire,
		last9api.WithQuery(in.query()), last9api.WithRequiredBody())
	if err != nil {
		return GetMonitorResponse{}, err
	}
	if wire.AlertRules == nil {
		return GetMonitorResponse{}, fmt.Errorf("%s: response has no alert_rules", opMonitor)
	}
	return GetMonitorResponse{AlertRules: *wire.AlertRules}, nil
}
