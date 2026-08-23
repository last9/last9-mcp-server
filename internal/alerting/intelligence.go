package alerting

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"last9-mcp/internal/constants"
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

		entity := map[string]any{"serviceName": serviceName}
		if env := strings.TrimSpace(args.Env); env != "" {
			entity["env"] = env
		}
		for k, v := range args.Attributes {
			if strings.TrimSpace(k) != "" {
				entity[k] = v
			}
		}

		payload, err := json.Marshal(AlertIntelligenceRequest{
			Operation: AlertIntelDescribeChart,
			Surface:   surface,
			ChartKey:  chartKey,
			Entity:    entity,
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
		b.WriteString("\n\nThis surface/chart_key/entity combination is not covered by the alert-coverage catalog. Verify the surface and chart_key names against the Last9 dashboard Discover page for this service. Genuinely uncataloged charts cannot be bound by the typed MCP create flow (it attaches alerts to existing KPIs only); alert them via the Last9 dashboard or API instead.")
	case classPermissions:
		b.WriteString("\n\nThe configured credentials are not permitted on this route: alert intelligence requires a token whose role and scopes clear its POST gate; viewer-role tokens are rejected.")
	default:
		if e.Class == classUpstream {
			b.WriteString("\n\nThis is an upstream server-side failure; retry once before reporting it.")
		}
	}
	return b.String()
}
