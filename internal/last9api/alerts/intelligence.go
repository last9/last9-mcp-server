package alerts

import (
	"context"
	"encoding/json"
	"net/http"

	"last9-mcp/internal/last9api"
)

const pathAlertIntelligence = "/alert_intelligence"

const opAlertIntelligence = "alert intelligence"

const (
	operationDescribeChart   = "describe_chart"
	operationCreateFromChart = "create_from_chart"
)

// IntelligenceRequest is the request body shared by both operations; Operation is set by
// the calling function. Create must not carry query, origin, or source keys.
type IntelligenceRequest struct {
	Operation         string         `json:"operation"`
	Surface           string         `json:"surface,omitempty"`
	ChartKey          string         `json:"chart_key,omitempty"`
	Entity            map[string]any `json:"entity,omitempty"`
	SignalKey         string         `json:"signal_key,omitempty"`
	Name              string         `json:"name,omitempty"`
	Threshold         string         `json:"threshold,omitempty"`
	ThresholdOperator string         `json:"threshold_operator,omitempty"`
	EvalWindow        int            `json:"eval_window,omitempty"`
	BadMinutes        int            `json:"bad_minutes,omitempty"`
	Severity          string         `json:"severity,omitempty"`
	GroupID           string         `json:"group_id,omitempty"`
	Description       string         `json:"description,omitempty"`
	last9api.CreatedVia
}

// IntelligenceResponse is a permissive decode target for both operations; unknown fields
// are ignored.
type IntelligenceResponse struct {
	ID      string   `json:"id,omitempty"`
	Signals []Signal `json:"signals,omitempty"`
	GroupID string   `json:"group_id,omitempty"`
	KPIID   string   `json:"kpi_id,omitempty"`
}

type Signal struct {
	Key            string `json:"key"`
	EvalQuery      string `json:"eval_query,omitempty"`
	CanonicalQuery string `json:"canonical_query,omitempty"`
	Unit           string `json:"unit,omitempty"`
	QueryKind      string `json:"query_kind,omitempty"`
}

// DescribeAlertChart calls POST /alert_intelligence with operation=describe_chart and
// returns the raw response body.
func DescribeAlertChart(
	ctx context.Context, c *last9api.Client, in IntelligenceRequest,
) (json.RawMessage, error) {
	in.Operation = operationDescribeChart
	return callIntelligence(ctx, c, in)
}

// CreateAlertFromChart calls POST /alert_intelligence with operation=create_from_chart and
// returns the raw response body, which decodes into IntelligenceResponse.
func CreateAlertFromChart(
	ctx context.Context, c *last9api.Client, in IntelligenceRequest,
) (json.RawMessage, error) {
	in.Operation = operationCreateFromChart
	return callIntelligence(ctx, c, in)
}

func callIntelligence(
	ctx context.Context, c *last9api.Client, in IntelligenceRequest,
) (json.RawMessage, error) {
	var out json.RawMessage
	err := c.Do(ctx, opAlertIntelligence,
		last9api.Request{Method: http.MethodPost, Path: pathAlertIntelligence, Body: in}, &out)
	if err != nil {
		return nil, err
	}
	return out, nil
}
