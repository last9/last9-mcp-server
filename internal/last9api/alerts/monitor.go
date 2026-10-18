// Package alerts holds the alerting endpoints.
package alerts

import (
	"context"
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

// GetMonitor calls GET /alerts/monitor.
func GetMonitor(ctx context.Context, c *last9api.Client, in GetMonitorInput) (GetMonitorResponse, error) {
	q := url.Values{}
	q.Set("timestamp", strconv.FormatInt(in.Timestamp, 10))
	q.Set("window", strconv.FormatInt(in.Window, 10))
	var resp GetMonitorResponse
	err := c.Do(ctx, opMonitor, last9api.Request{Method: http.MethodGet, Path: pathMonitor}, &resp, last9api.WithQuery(q))
	return resp, err
}
