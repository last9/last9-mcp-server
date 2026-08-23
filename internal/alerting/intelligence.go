package alerting

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"last9-mcp/internal/constants"
	"last9-mcp/internal/deeplink"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Alert intelligence operations served by the BFF /alert_intelligence endpoint.
const (
	AlertIntelDescribeChart   = "describe_chart"
	AlertIntelCreateFromChart = "create_from_chart"
)

// AlertIntelligenceRequest mirrors the upstream POST /alert_intelligence body
// (api/types/req/alert_intelligence.go). Create must not accept query, origin,
// or source keys — the BFF rejects them at decode.
type AlertIntelligenceRequest struct {
	Operation         string         `json:"operation"`
	Surface           string         `json:"surface,omitempty"`
	ChartKey          string         `json:"chart_key,omitempty"`
	Entity            map[string]any `json:"entity,omitempty"`
	SignalKey         string         `json:"signal_key,omitempty"`
	Name              string         `json:"name,omitempty"`
	Algorithm         string         `json:"algorithm,omitempty"`
	Threshold         string         `json:"threshold,omitempty"`
	ThresholdOperator string         `json:"threshold_operator,omitempty"`
	EvalWindow        int            `json:"eval_window,omitempty"`
	BadMinutes        int            `json:"bad_minutes,omitempty"`
	Severity          string         `json:"severity,omitempty"`
	GroupID           string         `json:"group_id,omitempty"`
}

// AlertIntelligenceResponse is a permissive decode target for the shared
// describe/create response (api/types/res/alert_intelligence.go); unknown
// fields are ignored.
type AlertIntelligenceResponse struct {
	ID       string                    `json:"id,omitempty"`
	Signals  []AlertIntelligenceSignal `json:"signals,omitempty"`
	Pointers AlertIntelPointers        `json:"pointers,omitempty"`
	GroupID  string                    `json:"group_id,omitempty"`
	KPIID    string                    `json:"kpi_id,omitempty"`
}

// AlertIntelligenceSignal is one catalog signal on the chart.
type AlertIntelligenceSignal struct {
	Key            string `json:"key"`
	EvalQuery      string `json:"eval_query,omitempty"`
	CanonicalQuery string `json:"canonical_query,omitempty"`
	Unit           string `json:"unit,omitempty"`
	QueryKind      string `json:"query_kind,omitempty"`
}

// AlertIntelPointers are inspect links returned with the response.
type AlertIntelPointers struct {
	ViewSource string `json:"view_source,omitempty"`
	ViewIn     string `json:"view_in,omitempty"`
}

// callAlertIntelligence POSTs an encoded request to the BFF alert
// intelligence endpoint using the same header, response-cap, and error
// truncation discipline as doAlertMutation. Empty payloads are rejected
// before any HTTP traffic.
func callAlertIntelligence(ctx context.Context, client *http.Client, cfg models.Config, payload []byte) ([]byte, error) {
	if len(bytes.TrimSpace(payload)) == 0 {
		return nil, fmt.Errorf("alert intelligence payload is required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.APIBaseURL+constants.EndpointAlertIntelligence, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("failed to create alert intelligence request: %w", err)
	}
	req.Header.Set(constants.HeaderAccept, constants.HeaderAcceptJSON)
	req.Header.Set(constants.HeaderContentType, constants.HeaderContentTypeJSON)
	req.Header.Set(constants.HeaderXLast9APIToken, constants.BearerPrefix+cfg.TokenManager.GetAccessToken(ctx))

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("alert intelligence request failed: %w", err)
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, maxAlertMutationResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("failed to read alert intelligence response: %w", err)
	}
	if int64(len(responseBody)) > maxAlertMutationResponseBytes {
		return nil, fmt.Errorf("alert intelligence response exceeds %d bytes", maxAlertMutationResponseBytes)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return nil, newAlertIntelHTTPError(resp.StatusCode, responseBody)
	}
	return responseBody, nil
}

// alertIntelErrorClass buckets /alert_intelligence failures so handlers can
// tailor responder guidance instead of surfacing raw transport noise.
type alertIntelErrorClass int

const (
	classUpstream      alertIntelErrorClass = iota
	classCoverageMiss                       // chart/signal/entity not covered by the catalog
	classPermissions                        // token lacks create/read scope for intelligence
	classDuplicateName                      // rule name already exists on the group
)

func (c alertIntelErrorClass) String() string {
	switch c {
	case classCoverageMiss:
		return "coverage_miss"
	case classPermissions:
		return "permissions"
	case classDuplicateName:
		return "duplicate_name"
	default:
		return "upstream"
	}
}

// Sentinels mirror upstream api/alertintelligence errors that map to 400
// BadRequest and mean "this chart is not alert-coverage eligible".
var alertIntelCoverageMissSentinels = []string{
	"unknown chart key",
	"unknown signal key",
	"invalid chart entity",
}

// classifyAlertIntelligenceError maps an HTTP status plus response body onto
// a stable failure class. Only 400s carrying a coverage-miss sentinel count
// as coverage misses; every other status — including bare 400s — is upstream.
func classifyAlertIntelligenceError(statusCode int, body string) alertIntelErrorClass {
	if statusCode == http.StatusBadRequest {
		for _, sentinel := range alertIntelCoverageMissSentinels {
			if strings.Contains(body, sentinel) {
				return classCoverageMiss
			}
		}
		return classUpstream
	}
	switch statusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return classPermissions
	case http.StatusConflict:
		return classDuplicateName
	default:
		return classUpstream
	}
}

// AlertIntelHTTPError carries the classified failure from
// callAlertIntelligence; handlers use errors.As to branch on Class.
type AlertIntelHTTPError struct {
	StatusCode int
	Class      alertIntelErrorClass
	Body       string
}

func newAlertIntelHTTPError(statusCode int, body []byte) *AlertIntelHTTPError {
	message := strings.TrimSpace(string(body))
	if len(message) > 4096 {
		message = message[:4096] + "..."
	}
	if message == "" {
		message = http.StatusText(statusCode)
	}
	return &AlertIntelHTTPError{
		StatusCode: statusCode,
		Class:      classifyAlertIntelligenceError(statusCode, string(body)),
		Body:       message,
	}
}

func (e *AlertIntelHTTPError) Error() string {
	return fmt.Sprintf("alert intelligence API returned status %d (%s): %s", e.StatusCode, e.Class, e.Body)
}

// DescribeAlertChartArgs holds the input arguments for describe_alert_chart.
type DescribeAlertChartArgs struct {
	Surface     string            `json:"surface" jsonschema:"(Required) Surface identifier; currently covered surfaces: discover-service, discover-exceptions"`
	ChartKey    string            `json:"chart_key" jsonschema:"(Required) Chart key within the surface, for example apdex, error_rate, response_time, exception_count"`
	ServiceName string            `json:"service_name" jsonschema:"(Required) Service/entity name the chart is scoped to"`
	Env         string            `json:"env,omitempty" jsonschema:"Environment when the chart distinguishes one, for example prod"`
	Attributes  map[string]string `json:"attributes,omitempty" jsonschema:"Extra identity dimensions some charts need; use exactly the key names describe reports"`
}

// alertIntelEntity builds the chart identity map shared by describe and
// create-from-chart: serviceName plus optional env and verbatim attributes.
func alertIntelEntity(serviceName, env string, attrs map[string]string) map[string]any {
	entity := map[string]any{"serviceName": serviceName}
	if env = strings.TrimSpace(env); env != "" {
		entity["env"] = env
	}
	for k, v := range attrs {
		if strings.TrimSpace(k) != "" {
			entity[k] = v
		}
	}
	return entity
}

// NewDescribeAlertChartHandler returns the MCP tool handler for
// describe_alert_chart: a read-only enumerate of the alertable signals on a
// covered chart.
func NewDescribeAlertChartHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, DescribeAlertChartArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args DescribeAlertChartArgs) (*mcp.CallToolResult, any, error) {
		surface := strings.TrimSpace(args.Surface)
		if surface == "" {
			return utils.ToolErrorResult("surface is required"), nil, nil
		}
		chartKey := strings.TrimSpace(args.ChartKey)
		if chartKey == "" {
			return utils.ToolErrorResult("chart_key is required"), nil, nil
		}
		serviceName := strings.TrimSpace(args.ServiceName)
		if serviceName == "" {
			return utils.ToolErrorResult("service_name is required"), nil, nil
		}

		payload, err := json.Marshal(AlertIntelligenceRequest{
			Operation: AlertIntelDescribeChart,
			Surface:   surface,
			ChartKey:  chartKey,
			Entity:    alertIntelEntity(serviceName, args.Env, args.Attributes),
		})
		if err != nil {
			return nil, nil, fmt.Errorf("failed to encode alert intelligence request: %w", err)
		}

		body, err := callAlertIntelligence(ctx, client, cfg, payload)
		if err != nil {
			var apiErr *AlertIntelHTTPError
			if errors.As(err, &apiErr) {
				return utils.ToolErrorResult(alertIntelGuidance(apiErr)), nil, nil
			}
			return nil, nil, err
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(body)}}}, nil, nil
	}
}

// alertIntelGuidance renders a classified /alert_intelligence failure as
// model-facing guidance: verbatim upstream reason plus class-specific forward
// motion. Retry framing only applies to the upstream class.
func alertIntelGuidance(e *AlertIntelHTTPError) string {
	var b strings.Builder
	fmt.Fprintf(&b, "alert intelligence API returned status %d (%s): %s", e.StatusCode, e.Class, e.Body)
	switch e.Class {
	case classCoverageMiss:
		b.WriteString("\n\nThis surface/chart_key/entity combination is not covered by the alert-coverage catalog. Re-run describe_alert_chart with these exact coordinates (surface, chart_key, env, attributes, signal_key) to see what the catalog covers, or verify the chart against the Last9 dashboard Discover page for this service. Genuinely uncataloged charts cannot be bound by the typed MCP create flow (it attaches alerts to existing KPIs only); alert them via the Last9 dashboard or API instead.")
	case classPermissions:
		b.WriteString("\n\nThe configured credentials are not permitted on this route: alert intelligence requires a token whose role and scopes clear its POST gate; viewer-role tokens are rejected.")
	case classDuplicateName:
		b.WriteString("\n\nAn alert rule with this name already exists. Check existing rules with get_entity_alert_rules and pick a different name before retrying. After a timeout, never retry blindly — verify with get_entity_alert_rules whether the rule was actually created first, or you will create a duplicate.")
	default:
		if e.Class == classUpstream {
			b.WriteString("\n\nThis is an upstream server-side failure; retry once before reporting it.")
		}
	}
	return b.String()
}

// Backend defaults the BFF applies when optional create fields are omitted;
// cited verbatim in results so every default is auditable.
const (
	defaultThresholdOperator = ">"
	defaultThreshold         = "0.01"
	defaultEvalWindow        = 5
	defaultBadMinutes        = 3
	defaultSeverity          = "breach"
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
}

// NewCreateAlertFromChartHandler returns the MCP tool handler for
// create_alert_from_chart: one-call static-threshold rule creation from a
// covered Discover chart identity.
func NewCreateAlertFromChartHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, CreateAlertFromChartArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args CreateAlertFromChartArgs) (*mcp.CallToolResult, any, error) {
		surface := strings.TrimSpace(args.Surface)
		if surface == "" {
			return utils.ToolErrorResult("surface is required"), nil, nil
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
		if args.Severity != "" && args.Severity != "breach" && args.Severity != "threat" {
			return utils.ToolErrorResult("severity must be breach or threat"), nil, nil
		}

		req := AlertIntelligenceRequest{
			Operation: AlertIntelCreateFromChart,
			Surface:   surface,
			ChartKey:  chartKey,
			Entity:    alertIntelEntity(serviceName, args.Env, args.Attributes),
			SignalKey: signalKey,
			Name:      name,
		}
		// Omitted optionals stay zero-valued so omitempty drops them from the
		// wire payload and the BFF/backend defaults apply untouched.
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

		payload, err := json.Marshal(req)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to encode alert intelligence request: %w", err)
		}

		body, err := callAlertIntelligence(ctx, client, cfg, payload)
		if err != nil {
			var apiErr *AlertIntelHTTPError
			if errors.As(err, &apiErr) {
				return utils.ToolErrorResult(alertIntelGuidance(apiErr)), nil, nil
			}
			return nil, nil, err
		}

		var resp AlertIntelligenceResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, nil, fmt.Errorf("failed to parse alert intelligence response: %w", err)
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
func createFromChartReport(resp AlertIntelligenceResponse, args CreateAlertFromChartArgs) string {
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
