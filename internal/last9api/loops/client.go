package loops

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"last9-mcp/internal/utils"
)

const (
	loopsNotEnabled   = "LOOPS_NOT_ENABLED"
	notEnabledMessage = "Loops is not enabled for this organization. Ask a Last9 admin to enable the Loops app, then retry."
	maxResponseBytes  = 1 << 20
)

// NotEnabledError is a soft, actionable disablement for Loops tools.
type NotEnabledError struct{}

func (e *NotEnabledError) Error() string { return notEnabledMessage }

// IsNotEnabled reports whether err means Loops is off for the org.
func IsNotEnabled(err error) bool {
	var n *NotEnabledError
	return errors.As(err, &n)
}

// StatusResponse is the structured body for get_loops_status.
func StatusResponse(enabled bool, err error) Status {
	if enabled {
		return Status{Enabled: true}
	}
	reason := loopsNotEnabled
	msg := notEnabledMessage
	if err != nil && !IsNotEnabled(err) {
		msg = err.Error()
		reason = "upstream_error"
	}
	return Status{Enabled: false, Reason: reason, Message: msg}
}

func mapError(err error) error {
	if err == nil {
		return nil
	}
	if loopsDisabled(err) {
		return &NotEnabledError{}
	}
	return err
}

func loopsDisabled(err error) bool {
	var upstream *utils.UpstreamHTTPError
	if !errors.As(err, &upstream) {
		return false
	}
	if upstream.StatusCode != http.StatusNotFound && upstream.StatusCode != http.StatusUnprocessableEntity {
		return false
	}
	return loopsErrorCode(upstream.Body) == loopsNotEnabled
}

func loopsErrorCode(body string) string {
	var envelope struct {
		Code         string `json:"code"`
		ErrorDetails *struct {
			Code string `json:"code"`
		} `json:"error_details"`
		Error any `json:"error"`
	}
	if json.Unmarshal([]byte(body), &envelope) != nil {
		return ""
	}
	if envelope.ErrorDetails != nil && envelope.ErrorDetails.Code != "" {
		return strings.ToUpper(envelope.ErrorDetails.Code)
	}
	if envelope.Code != "" {
		return strings.ToUpper(envelope.Code)
	}
	switch v := envelope.Error.(type) {
	case string:
		if strings.EqualFold(strings.TrimSpace(v), loopsNotEnabled) {
			return loopsNotEnabled
		}
	case map[string]any:
		if code, ok := v["code"].(string); ok {
			return strings.ToUpper(code)
		}
	}
	return ""
}

func filterSummaries(items []Summary, query string) []Summary {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return items
	}
	out := make([]Summary, 0, len(items))
	for _, item := range items {
		if strings.Contains(strings.ToLower(item.Name), q) || strings.Contains(strings.ToLower(item.ID), q) {
			out = append(out, item)
		}
	}
	return out
}

func filterTemplates(items []Template, capability string) []Template {
	c := strings.ToLower(strings.TrimSpace(capability))
	if c == "" {
		return items
	}
	out := make([]Template, 0, len(items))
	for _, item := range items {
		if strings.EqualFold(item.Capability, c) || strings.Contains(strings.ToLower(item.Slug), c) {
			out = append(out, item)
		}
	}
	return out
}

func decodeSummaries(raw json.RawMessage) ([]Summary, error) {
	raw = unwrapArray(raw, "loops", "automations", "workflows", "items")
	var wire []wireLoop
	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, fmt.Errorf("decode list: %w", err)
	}
	out := make([]Summary, 0, len(wire))
	for _, w := range wire {
		out = append(out, w.toSummary())
	}
	return out, nil
}

func decodeTemplates(raw json.RawMessage) ([]Template, error) {
	raw = unwrapArray(raw, "templates", "items")
	var wire []wireTemplate
	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, fmt.Errorf("decode templates: %w", err)
	}
	out := make([]Template, 0, len(wire))
	for _, w := range wire {
		out = append(out, w.toTemplate())
	}
	return out, nil
}

func decodeDetail(raw json.RawMessage) (*Detail, error) {
	var w wireLoop
	if err := json.Unmarshal(raw, &w); err != nil {
		return nil, fmt.Errorf("decode loop: %w", err)
	}
	detail := &Detail{Summary: w.toSummary(), SetupGaps: setupGaps(w), Graph: graphSummary(w.Graph)}
	return detail, nil
}

func unwrapArray(raw json.RawMessage, keys ...string) json.RawMessage {
	trimmed := strings.TrimSpace(string(raw))
	if strings.HasPrefix(trimmed, "[") {
		return raw
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil {
		return raw
	}
	for _, key := range keys {
		if v, ok := obj[key]; ok {
			return v
		}
	}
	return raw
}

type wireLoop struct {
	ID              string          `json:"id"`
	Name            string          `json:"name"`
	Status          string          `json:"status"`
	Enabled         *bool           `json:"enabled"`
	ActionMode      string          `json:"action_mode"`
	ActionModeCamel string          `json:"actionMode"`
	LastRun         json.RawMessage `json:"last_run"`
	LastRunCamel    json.RawMessage `json:"lastRun"`
	Graph           json.RawMessage `json:"graph"`
	SetupGaps       []string        `json:"setup_gaps"`
}

func (w wireLoop) toSummary() Summary {
	enabled := false
	if w.Enabled != nil {
		enabled = *w.Enabled
	}
	mode := w.ActionMode
	if mode == "" {
		mode = w.ActionModeCamel
	}
	return Summary{
		ID:         w.ID,
		Name:       w.Name,
		Status:     w.Status,
		Enabled:    enabled,
		ActionMode: mode,
		LastRun:    decodeLastRun(firstRaw(w.LastRun, w.LastRunCamel)),
	}
}

func firstRaw(a, b json.RawMessage) json.RawMessage {
	if len(a) > 0 && string(a) != "null" {
		return a
	}
	return b
}

func decodeLastRun(raw json.RawMessage) *LastRun {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var wire struct {
		ID             string `json:"id"`
		Status         string `json:"status"`
		StartedAt      string `json:"started_at"`
		StartedAtCamel string `json:"startedAt"`
	}
	if json.Unmarshal(raw, &wire) != nil {
		return nil
	}
	started := wire.StartedAt
	if started == "" {
		started = wire.StartedAtCamel
	}
	if wire.ID == "" && wire.Status == "" && started == "" {
		return nil
	}
	return &LastRun{ID: wire.ID, Status: wire.Status, StartedAt: started}
}

type wireTemplate struct {
	Slug                string   `json:"slug"`
	Name                string   `json:"name"`
	Description         string   `json:"description"`
	RequiredParams      []string `json:"required_params"`
	RequiredParamsCamel []string `json:"requiredParams"`
	Connectors          []string `json:"connectors"`
	Capability          string   `json:"capability"`
}

func (w wireTemplate) toTemplate() Template {
	params := w.RequiredParams
	if len(params) == 0 {
		params = w.RequiredParamsCamel
	}
	return Template{
		Slug:           w.Slug,
		Name:           w.Name,
		Description:    w.Description,
		RequiredParams: params,
		Connectors:     w.Connectors,
		Capability:     w.Capability,
	}
}

func setupGaps(w wireLoop) []string {
	if len(w.SetupGaps) > 0 {
		return append([]string(nil), w.SetupGaps...)
	}
	gaps := []string{}
	if len(w.Graph) == 0 || string(w.Graph) == "null" || string(w.Graph) == "{}" || string(w.Graph) == "[]" {
		gaps = append(gaps, "graph is empty; create from a template or edit in the Loops UI")
	}
	lower := strings.ToLower(string(w.Graph))
	if strings.Contains(lower, "slack") && (strings.Contains(lower, `"channel":""`) || strings.Contains(lower, `"channel": null`)) {
		gaps = append(gaps, "slack channel is unset")
	}
	return gaps
}

func graphSummary(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return nil
	}
	redacted := redactValue(v)
	out, err := json.Marshal(redacted)
	if err != nil || len(out) > 8<<10 {
		return json.RawMessage(`{"truncated":true}`)
	}
	return out
}

func redactValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		redactGenericValue := hasSecretNameValue(t)
		out := make(map[string]any, len(t))
		for k, child := range t {
			if shouldRedact(k, redactGenericValue) {
				out[k] = "[redacted]"
				continue
			}
			out[k] = redactValue(child)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, child := range t {
			out[i] = redactValue(child)
		}
		return out
	default:
		return v
	}
}

func shouldRedact(key string, genericValue bool) bool {
	return secretKey(key) ||
		strings.EqualFold(key, "url") ||
		genericValue && strings.EqualFold(key, "value")
}

func hasSecretNameValue(v map[string]any) bool {
	for key, raw := range v {
		value, ok := raw.(string)
		if ok && (strings.EqualFold(key, "key") || strings.EqualFold(key, "name") || strings.EqualFold(key, "header")) && secretKey(value) {
			return true
		}
	}
	return false
}

func secretKey(k string) bool {
	lower := strings.ToLower(k)
	for _, needle := range []string{"token", "secret", "password", "authorization", "webhook", "api_key", "api-key", "apikey"} {
		if strings.Contains(lower, needle) {
			return true
		}
	}
	return false
}
