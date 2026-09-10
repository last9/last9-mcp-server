package dashboards

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"last9-mcp/internal/constants"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"
)

// Execution + classification helpers for validate_dashboard.
// Port of agents/supervisor/skills/dashboard_validation/executor.py (day-1: no probes).

const (
	logsExecutionLimit = 100
	headSampleN        = 3
	errorClipLimit     = 500
)

var instantVizTypes = map[string]struct{}{
	"stat":  {},
	"gauge": {},
	"table": {},
}

var syntaxErrorMarkers = []string{
	"parse error",
	"invalid expression",
	"unexpected token",
	"bad_data",
	"invalid parameter",
}

type executionOutcome struct {
	status     string // executed | invalid_query | execution_error | source_unavailable
	tool       string
	payload    any
	errorText  string
	durationMs int
}

type validateExecutor struct {
	client *http.Client
	cfg    models.Config
}

func (e *validateExecutor) fetchDashboard(ctx context.Context, dashboardID string) (map[string]any, string) {
	region, err := resolveRegion(e.cfg, "")
	if err != nil {
		return nil, err.Error()
	}
	path := fmt.Sprintf(constants.EndpointDashboardByID, url.PathEscape(dashboardID))
	u := e.cfg.APIBaseURL + path + "?" + url.Values{"region": {region}}.Encode()

	started := time.Now()
	body, _, err := doJSONRequest(ctx, e.client, e.cfg, http.MethodGet, u, nil)
	_ = started
	if err != nil {
		return nil, clipError(err.Error())
	}
	var payload any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, "get_dashboard returned a non-object payload"
	}
	obj, ok := payload.(map[string]any)
	if !ok {
		return nil, "get_dashboard returned a non-object payload"
	}
	dashboard, _ := obj["dashboard"].(map[string]any)
	if dashboard == nil {
		dashboard = obj
	}
	if dashboard == nil {
		return nil, "get_dashboard payload has no dashboard object"
	}
	return dashboard, ""
}

func (e *validateExecutor) runPromQL(ctx context.Context, expr string, window map[string]string, vizType string) executionOutcome {
	end, err := parseValidateISO(window["end"])
	if err != nil {
		return executionOutcome{status: "execution_error", tool: "prometheus_range_query", errorText: err.Error()}
	}
	endUnix := end.Unix()

	var tool string
	var httpResp *http.Response
	var callErr error
	started := time.Now()

	if _, ok := instantVizTypes[strings.ToLower(vizType)]; ok {
		tool = "prometheus_instant_query"
		httpResp, callErr = utils.MakePromInstantAPIQuery(ctx, e.client, expr, endUnix, e.cfg)
	} else {
		tool = "prometheus_range_query"
		start, err := parseValidateISO(window["start"])
		if err != nil {
			return executionOutcome{status: "execution_error", tool: tool, errorText: err.Error()}
		}
		httpResp, callErr = utils.MakePromRangeAPIQuery(ctx, e.client, expr, start.Unix(), endUnix, e.cfg, utils.PromResolution{})
	}
	duration := int(time.Since(started).Milliseconds())
	if callErr != nil {
		return executionOutcome{status: "source_unavailable", tool: tool, errorText: clipError(callErr.Error()), durationMs: duration}
	}
	if httpResp == nil {
		return executionOutcome{status: "source_unavailable", tool: tool, errorText: "received nil response", durationMs: duration}
	}
	defer httpResp.Body.Close()
	rawBody, err := io.ReadAll(io.LimitReader(httpResp.Body, maxAPISuccessBodyBytes+1))
	if err != nil {
		return executionOutcome{status: "source_unavailable", tool: tool, errorText: clipError(err.Error()), durationMs: duration}
	}
	if httpResp.StatusCode != http.StatusOK {
		return outcomeFromHTTPError(tool, string(rawBody), httpResp.StatusCode, duration)
	}
	return outcomeFromResult(tool, rawBody, "", duration)
}

func (e *validateExecutor) runLogJSON(ctx context.Context, pipeline any, window map[string]string, index string) executionOutcome {
	tool := "get_logs"
	start, err := parseValidateISO(window["start"])
	if err != nil {
		return executionOutcome{status: "execution_error", tool: tool, errorText: err.Error()}
	}
	end, err := parseValidateISO(window["end"])
	if err != nil {
		return executionOutcome{status: "execution_error", tool: tool, errorText: err.Error()}
	}
	started := time.Now()
	httpResp, callErr := utils.MakeLogsJSONQueryAPI(
		ctx, e.client, e.cfg, pipeline,
		start.UnixMilli(), end.UnixMilli(),
		logsExecutionLimit, index,
	)
	duration := int(time.Since(started).Milliseconds())
	if callErr != nil {
		return executionOutcome{status: "source_unavailable", tool: tool, errorText: clipError(callErr.Error()), durationMs: duration}
	}
	if httpResp == nil {
		return executionOutcome{status: "source_unavailable", tool: tool, errorText: "received nil response", durationMs: duration}
	}
	defer httpResp.Body.Close()
	rawBody, err := io.ReadAll(io.LimitReader(httpResp.Body, maxAPISuccessBodyBytes+1))
	if err != nil {
		return executionOutcome{status: "source_unavailable", tool: tool, errorText: clipError(err.Error()), durationMs: duration}
	}
	if httpResp.StatusCode != http.StatusOK {
		return outcomeFromHTTPError(tool, string(rawBody), httpResp.StatusCode, duration)
	}
	return outcomeFromResult(tool, rawBody, "", duration)
}

func outcomeFromHTTPError(tool, body string, statusCode, durationMs int) executionOutcome {
	errText := clipError(fmt.Sprintf("HTTP %d: %s", statusCode, body))
	lowered := strings.ToLower(errText)
	status := "execution_error"
	for _, marker := range syntaxErrorMarkers {
		if strings.Contains(lowered, marker) {
			status = "invalid_query"
			break
		}
	}
	return executionOutcome{status: status, tool: tool, errorText: errText, durationMs: durationMs}
}

func outcomeFromResult(tool string, raw []byte, transportErr string, durationMs int) executionOutcome {
	if transportErr != "" {
		return executionOutcome{status: "source_unavailable", tool: tool, errorText: transportErr, durationMs: durationMs}
	}
	payload := parseJSONPayload(raw)
	if errText := errorTextFromPayload(payload, string(raw)); errText != "" {
		lowered := strings.ToLower(errText)
		status := "execution_error"
		for _, marker := range syntaxErrorMarkers {
			if strings.Contains(lowered, marker) {
				status = "invalid_query"
				break
			}
		}
		return executionOutcome{status: status, tool: tool, errorText: errText, durationMs: durationMs}
	}
	return executionOutcome{status: "executed", tool: tool, payload: payload, durationMs: durationMs}
}

func parseJSONPayload(raw []byte) any {
	if len(raw) == 0 {
		return nil
	}
	var payload any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil
	}
	return payload
}

func errorTextFromPayload(payload any, raw string) string {
	if obj, ok := payload.(map[string]any); ok {
		if isTruthy(obj["is_error"]) || isTruthy(obj["isError"]) {
			if v := obj["error"]; v != nil {
				return fmt.Sprint(v)
			}
			if v := obj["message"]; v != nil {
				return fmt.Sprint(v)
			}
			return "tool error"
		}
		if err := obj["error"]; err != nil && err != "" {
			return fmt.Sprint(err)
		}
		if status, _ := obj["status"].(string); status == "error" {
			if v := obj["errorType"]; v != nil {
				return fmt.Sprint(v)
			}
			if v := obj["message"]; v != nil {
				return fmt.Sprint(v)
			}
			return "backend error"
		}
		if okVal, isBool := obj["ok"].(bool); isBool && !okVal {
			if v := obj["detail"]; v != nil {
				return fmt.Sprint(v)
			}
			return "tool error"
		}
	}
	trimmed := strings.TrimSpace(raw)
	if strings.HasPrefix(strings.ToLower(trimmed), "error") {
		return clipError(trimmed)
	}
	return ""
}

func isTruthy(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return t != "" && t != "false" && t != "0"
	case float64:
		return t != 0
	case nil:
		return false
	default:
		return t != nil
	}
}

func extractResultRows(payload any) []any {
	if payload == nil {
		return nil
	}
	if list, ok := payload.([]any); ok {
		return list
	}
	obj, ok := payload.(map[string]any)
	if !ok {
		return nil
	}
	if data, ok := obj["data"].(map[string]any); ok {
		if result, ok := data["result"].([]any); ok {
			return result
		}
	}
	for _, key := range []string{"result", "logs", "entries", "rows"} {
		if list, ok := obj[key].([]any); ok {
			return list
		}
	}
	return nil
}

func hasData(payload any) bool {
	rows := extractResultRows(payload)
	return len(rows) > 0
}

func boundedEvidence(payload any) map[string]any {
	rows := extractResultRows(payload)
	if rows == nil {
		return map[string]any{
			"sample_count": 0,
			"total_count":  0,
			"truncated":    false,
			"samples":      []any{},
			"note":         "unrecognized payload shape; treated as empty",
		}
	}
	total := len(rows)
	alreadyTruncated := false
	if obj, ok := payload.(map[string]any); ok && isTruthy(obj["truncated"]) {
		alreadyTruncated = true
		switch t := obj["total_entries"].(type) {
		case float64:
			total = int(t)
		case int:
			total = t
		case map[string]any:
			max := total
			for _, v := range t {
				if n := asInt(v); n > max {
					max = n
				}
			}
			total = max
		}
	}
	n := headSampleN
	if n > len(rows) {
		n = len(rows)
	}
	samples := compactSamples(rows[:n])
	return map[string]any{
		"sample_count": len(samples),
		"total_count":  total,
		"truncated":    alreadyTruncated || total > len(samples),
		"samples":      samples,
	}
}

func compactSamples(samples []any) []any {
	out := make([]any, 0, len(samples))
	for _, item := range samples {
		obj, ok := item.(map[string]any)
		if !ok {
			out = append(out, map[string]any{"row": clipError(fmt.Sprint(item))})
			continue
		}
		compact := map[string]any{}
		if metric, ok := obj["metric"].(map[string]any); ok {
			compact["labels"] = metric
		}
		if values, ok := obj["values"].([]any); ok {
			compact["points"] = len(values)
			if len(values) > 0 {
				compact["last_value"] = values[len(values)-1]
			} else {
				compact["last_value"] = nil
			}
		}
		if value, ok := obj["value"]; ok {
			compact["value"] = value
		}
		if len(compact) == 0 {
			b, _ := json.Marshal(obj)
			compact = map[string]any{"row": clipError(string(b))}
		}
		out = append(out, compact)
	}
	return out
}

func clipError(text string) string {
	if len(text) <= errorClipLimit {
		return text
	}
	return text[:errorClipLimit] + "…"
}
