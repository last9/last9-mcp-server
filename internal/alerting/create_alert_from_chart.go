package alerting

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"last9-mcp/internal/deeplink"
	"last9-mcp/internal/last9api"
	"last9-mcp/internal/last9api/alerts"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Backend defaults applied when optional create fields are omitted;
// cited verbatim in results so every default is auditable.
const (
	defaultThresholdOperator = ">"
	defaultThreshold         = "0.01"
	defaultEvalWindow        = 5
	defaultBadMinutes        = 3
	defaultSeverity          = severityBreach
)

var validThresholdOperators = map[string]bool{
	">": true, "<": true, ">=": true, "<=": true, "==": true, "!=": true,
}

// CreateAlertFromChartArgs holds the input arguments for
// create_alert_from_chart: chart identity plus flat rule-editor fields.
type CreateAlertFromChartArgs struct {
	Surface           string            `json:"surface" jsonschema:"(Required) Surface identifier; one of discover-service or discover-exceptions"`
	ChartKey          string            `json:"chart_key" jsonschema:"(Required) Chart key within the surface, for example apdex, error_rate, response_time, exception_count"`
	ServiceName       string            `json:"service_name" jsonschema:"(Required) Service/entity name the chart is scoped to"`
	SignalKey         string            `json:"signal_key" jsonschema:"(Required) Signal key returned by describe_alert_chart for this chart"`
	Name              string            `json:"name" jsonschema:"(Required) Alert rule name; duplicate names are rejected with a conflict"`
	Env               string            `json:"env,omitempty" jsonschema:"Environment when the chart distinguishes one, for example prod"`
	Attributes        map[string]string `json:"attributes,omitempty" jsonschema:"Extra identity dimensions some charts need; use exactly the key names describe_alert_chart reports"`
	Threshold         *float64          `json:"threshold,omitempty" jsonschema:"Breach threshold in the signal's own unit as reported by describe_alert_chart; omit for the backend default 0.01 (rate/count signals only — score-unit signals like apdex need an explicit threshold)"`
	ThresholdOperator string            `json:"threshold_operator,omitempty" jsonschema:"Comparison operator: > < >= <= == or !=; default >"`
	EvalWindow        *int              `json:"eval_window,omitempty" jsonschema:"Evaluation window in minutes, range 1-60; default 5"`
	BadMinutes        *int              `json:"bad_minutes,omitempty" jsonschema:"Minutes within eval_window the condition must hold before firing, range 1-eval_window; default 3"`
	Severity          string            `json:"severity,omitempty" jsonschema:"Severity: breach or threat; when omitted the backend derives it from the algorithm and static chart rules default to breach"`
	GroupID           string            `json:"group_id,omitempty" jsonschema:"Alert group UUID the chart resolves to; normally omit. The backend rejects any group other than the chart's own, so use only a group_id already returned for this chart"`
	Description       string            `json:"description,omitempty" jsonschema:"Responder-facing symptom, impact, and intended action stored on the rule as properties.description"`
}

// NewCreateAlertFromChartHandler returns the MCP tool handler for
// create_alert_from_chart: one-call static-threshold rule creation from a
// covered Discover chart identity.
func NewCreateAlertFromChartHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, CreateAlertFromChartArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, mcpReq *mcp.CallToolRequest, args CreateAlertFromChartArgs) (*mcp.CallToolResult, any, error) {
		surface := strings.TrimSpace(args.Surface)
		if surface == "" {
			return utils.ToolErrorResult("surface is required"), nil, nil
		}
		if msg := uncoveredSurfaceError(surface); msg != "" {
			return utils.ToolErrorResult(msg), nil, nil
		}
		chartKey := strings.TrimSpace(args.ChartKey)
		if chartKey == "" {
			return utils.ToolErrorResult("chart_key is required"), nil, nil
		}
		serviceName := strings.TrimSpace(args.ServiceName)
		if serviceName == "" {
			return utils.ToolErrorResult("service_name is required"), nil, nil
		}
		signalKey := strings.TrimSpace(args.SignalKey)
		if signalKey == "" {
			return utils.ToolErrorResult("signal_key is required"), nil, nil
		}
		name := strings.TrimSpace(args.Name)
		if name == "" {
			return utils.ToolErrorResult("name is required"), nil, nil
		}
		if args.ThresholdOperator != "" && !validThresholdOperators[args.ThresholdOperator] {
			return utils.ToolErrorResult("threshold_operator must be one of > < >= <= == !="), nil, nil
		}
		if args.Threshold != nil && *args.Threshold < 0 {
			return utils.ToolErrorResult("threshold must be a non-negative number"), nil, nil
		}
		evalWindow := defaultEvalWindow
		if args.EvalWindow != nil {
			if *args.EvalWindow < 1 || *args.EvalWindow > 60 {
				return utils.ToolErrorResult("eval_window must be between 1 and 60 minutes"), nil, nil
			}
			evalWindow = *args.EvalWindow
		}
		if args.BadMinutes != nil && (*args.BadMinutes < 1 || *args.BadMinutes > evalWindow) {
			return utils.ToolErrorResult(fmt.Sprintf("bad_minutes must be between 1 and eval_window (%d minutes)", evalWindow)), nil, nil
		}
		if args.Severity != "" && args.Severity != severityBreach && args.Severity != severityThreat {
			return utils.ToolErrorResult("severity must be breach or threat"), nil, nil
		}

		req := alerts.IntelligenceRequest{
			Surface:    surface,
			ChartKey:   chartKey,
			Entity:     alertIntelEntity(serviceName, args.Env, args.Attributes),
			SignalKey:  signalKey,
			Name:       name,
			CreatedVia: newCreatedViaFields(mcpReq),
		}
		// Omitted optionals stay zero-valued so omitempty drops them from the
		// wire payload and the backend defaults apply untouched.
		if args.Threshold != nil {
			req.Threshold = strconv.FormatFloat(*args.Threshold, 'f', -1, 64)
		}
		req.ThresholdOperator = args.ThresholdOperator
		if args.EvalWindow != nil {
			req.EvalWindow = *args.EvalWindow
		}
		if args.BadMinutes != nil {
			req.BadMinutes = *args.BadMinutes
		}
		req.Severity = args.Severity
		req.GroupID = strings.TrimSpace(args.GroupID)
		req.Description = args.Description

		body, err := alerts.CreateAlertFromChart(ctx, last9api.NewClient(client, cfg), req)
		if err != nil {
			var apiErr *utils.UpstreamHTTPError
			if errors.As(err, &apiErr) {
				return utils.ToolErrorResult(alertIntelGuidance(apiErr)), nil, nil
			}
			return utils.ToolErrorResult("alert intelligence request failed at the transport level and may or may not have been applied - verify whether the rule exists (get_alert_config filtered by rule name) before retrying"), nil, nil
		}

		var resp alerts.IntelligenceResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, nil, fmt.Errorf("failed to parse alert intelligence response: %w", err)
		}
		if strings.TrimSpace(resp.ID) == "" {
			return utils.ToolErrorResult("alert intelligence create returned success but no rule id; do not assume the rule was created - verify via get_alert_config before retrying"), nil, nil
		}
		link := deeplink.NewBuilder(cfg.OrgSlug, cfg.ClusterID).BuildAlertingGroupsLink()
		return &mcp.CallToolResult{
			Meta:    deeplink.ToMeta(link),
			Content: []mcp.Content{&mcp.TextContent{Text: createFromChartReport(resp, args)}},
		}, nil, nil
	}
}

// createFromChartReport renders the post-create summary: rule/group linkage,
// the effective condition with each applied backend default called out, and
// an immediate-fire note so callers know the rule is live.
func createFromChartReport(resp alerts.IntelligenceResponse, args CreateAlertFromChartArgs) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Created alert rule %s", resp.ID)
	if resp.GroupID != "" {
		fmt.Fprintf(&b, " in group %s", resp.GroupID)
	}
	if resp.KPIID != "" {
		fmt.Fprintf(&b, " (KPI %s)", resp.KPIID)
	}
	fmt.Fprintf(&b, " for signal %s on %s/%s.", args.SignalKey, strings.TrimSpace(args.Surface), strings.TrimSpace(args.ChartKey))

	settings := make([]string, 0, 5)
	setting := func(label, value string, defaulted bool) {
		if defaulted {
			value += " (backend default)"
		}
		settings = append(settings, "- "+label+": "+value)
	}
	op := args.ThresholdOperator
	if op == "" {
		op = defaultThresholdOperator
	}
	setting("threshold_operator", op, args.ThresholdOperator == "")
	threshold := defaultThreshold
	if args.Threshold != nil {
		threshold = strconv.FormatFloat(*args.Threshold, 'f', -1, 64)
	}
	setting("threshold", threshold, args.Threshold == nil)
	window := defaultEvalWindow
	if args.EvalWindow != nil {
		window = *args.EvalWindow
	}
	setting("eval_window", fmt.Sprintf("%d minutes", window), args.EvalWindow == nil)
	bad := defaultBadMinutes
	if args.BadMinutes != nil {
		bad = *args.BadMinutes
	}
	setting("bad_minutes", strconv.Itoa(bad), args.BadMinutes == nil)
	severity := args.Severity
	if severity == "" {
		setting("severity", defaultSeverity+" (backend default for static chart rules)", false)
	} else {
		setting("severity", severity, false)
	}

	b.WriteString("\n\nApplied settings:\n")
	b.WriteString(strings.Join(settings, "\n"))
	b.WriteString("\n\nThis rule is active now and can fire immediately — delete_alert removes it.")
	return b.String()
}
