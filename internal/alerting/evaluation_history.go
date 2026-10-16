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
	"last9-mcp/internal/models"
)

const (
	recentEvaluationHistoryPoints = 5
	recentEvaluationHistoryStep   = 60 // seconds between samples
	// Cap so a broad get_alert_config listing does not issue 5×N monitor calls.
	recentEvaluationHistoryMaxRules = 25
)

type evaluationSample struct {
	Timestamp int64
	State     string
}

func formatEvaluationHistoryLines(samples []evaluationSample) []string {
	if len(samples) == 0 {
		return nil
	}
	lines := make([]string, 0, len(samples)+1)
	lines = append(lines, "  Recent evaluations (newest first):")
	for _, s := range samples {
		ts := time.Unix(s.Timestamp, 0).UTC().Format("2006-01-02 15:04:05 UTC")
		lines = append(lines, fmt.Sprintf("    %s: %s", ts, s.State))
	}
	return lines
}

// Samples the alerts monitor API at recentEvaluationHistoryPoints timestamps so
// get_alert_config can show firing/ok history without a follow-up tool call.
// Failures are soft: callers omit history for affected rules.
func fetchRecentEvaluationHistory(
	ctx context.Context,
	client *http.Client,
	cfg models.Config,
	ruleIDs []string,
) map[string][]evaluationSample {
	if len(ruleIDs) == 0 || len(ruleIDs) > recentEvaluationHistoryMaxRules {
		return nil
	}
	wanted := make(map[string]struct{}, len(ruleIDs))
	for _, id := range ruleIDs {
		if id != "" {
			wanted[id] = struct{}{}
		}
	}
	if len(wanted) == 0 {
		return nil
	}

	now := time.Now().Unix()
	out := make(map[string][]evaluationSample, len(wanted))
	for i := 0; i < recentEvaluationHistoryPoints; i++ {
		ts := now - int64(i)*recentEvaluationHistoryStep
		states, err := fetchAlertMonitorStates(ctx, client, cfg, ts)
		if err != nil {
			continue
		}
		for ruleID := range wanted {
			// Absence is not proof of health: get_alert_rule_state only records
			// rules the monitor returned. Label unobserved minutes unknown.
			state := "unknown"
			if s, ok := states[ruleID]; ok && s != "" {
				state = s
			}
			out[ruleID] = append(out[ruleID], evaluationSample{Timestamp: ts, State: state})
		}
	}
	return out
}

func fetchAlertMonitorStates(
	ctx context.Context,
	client *http.Client,
	cfg models.Config,
	timestamp int64,
) (map[string]string, error) {
	q := url.Values{}
	q.Set("timestamp", fmt.Sprintf("%d", timestamp))
	q.Set("window", fmt.Sprintf("%d", recentEvaluationHistoryStep))
	fullURL := cfg.APIBaseURL + constants.EndpointAlertsMonitor + "?" + q.Encode()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set(constants.HeaderAccept, constants.HeaderAcceptJSON)
	httpReq.Header.Set(constants.HeaderXLast9APIToken, constants.BearerPrefix+cfg.TokenManager.GetAccessToken(ctx))

	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("API returned %d: %s", resp.StatusCode, string(body))
	}

	var alertResp AlertsResponse
	if err := json.Unmarshal(body, &alertResp); err != nil {
		return nil, err
	}
	states := make(map[string]string, len(alertResp.AlertRules))
	for _, rule := range alertResp.AlertRules {
		if rule.RuleID == "" {
			continue
		}
		state := rule.State
		if state == "" {
			state = "ok"
		}
		// The monitor can return one row per severity for the same rule ID; a
		// firing row must not be overwritten by a resolved row for the same rule.
		if existing, ok := states[rule.RuleID]; ok && existing == "firing" && state != "firing" {
			continue
		}
		states[rule.RuleID] = state
	}
	return states, nil
}
