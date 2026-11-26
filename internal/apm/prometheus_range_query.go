package apm

import (
	"context"
	"fmt"
	"net/http"

	"last9-mcp/internal/last9api"
	"last9-mcp/internal/last9api/prom"
	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type PromqlRangeQueryArgs struct {
	Query           string  `json:"query" jsonschema:"(Required) PromQL query to execute"`
	StartTimeISO    string  `json:"start_time_iso,omitempty" jsonschema:"Start time in RFC3339/ISO8601 format (e.g. 2024-06-01T12:00:00Z). Optional when lookback_minutes is provided."`
	EndTimeISO      string  `json:"end_time_iso,omitempty" jsonschema:"End time in RFC3339/ISO8601 format (e.g. 2024-06-01T13:00:00Z). Defaults to now when omitted."`
	LookbackMinutes float64 `json:"lookback_minutes,omitempty" jsonschema:"Number of minutes to look back from now (default: 60, minimum: 1). Use for relative windows like last 30 minutes."`
	Step            int64   `json:"step,omitempty" jsonschema:"Seconds between returned samples; omitted or 0 uses the default resolution."`
	Datasource      string  `json:"datasource,omitempty" jsonschema:"Datasource name; omitted uses the default, a near miss its closest match (named in a second content block)."`
}

func NewPromqlRangeQueryHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, PromqlRangeQueryArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args PromqlRangeQueryArgs) (*mcp.CallToolResult, any, error) {
		query := args.Query
		if query == "" {
			return nil, nil, fmt.Errorf("query is required")
		}

		startTimeParam, endTimeParam, err := resolveTimeRange(args.StartTimeISO, args.EndTimeISO, args.LookbackMinutes)
		if err != nil {
			return nil, nil, err
		}
		window := endTimeParam - startTimeParam
		if err := validateRangeStep(args.Step, window); err != nil {
			return nil, nil, err
		}

		choice, err := chooseDatasource(cfg, args.Datasource)
		if err != nil {
			return nil, nil, err
		}
		if choice.candidates != nil {
			return ambiguousDatasourceResult(args.Datasource, choice), nil, nil
		}
		raw, err := prom.Query(ctx, last9api.NewClient(client, choice.cfg), prom.QueryInput{
			Datasource: promDatasource(choice.cfg),
			Query:      query,
			Timestamp:  float64(endTimeParam),
			Window:     float64(window),
			Step:       args.Step,
		})
		res, out, err := promToolResult(raw, err, choice.note)
		return withMetricsLink(res, choice.cfg, metricsLink{StartSec: startTimeParam, EndSec: endTimeParam, Query: query}), out, err
	}
}

// Prometheus rejects a range query resolving to more than 11,000 points per series.
const promRangeMaxPoints = 11000

func validateRangeStep(step, window int64) error {
	switch {
	case step == 0:
		return nil
	case step < 0:
		return fmt.Errorf("step must be a positive number of seconds, or 0/omitted for the default resolution; got %d", step)
	case step > window:
		return fmt.Errorf("step %ds must be at most the %ds query window; lower step or widen the window", step, window)
	case window/step > promRangeMaxPoints:
		minStep := (window + promRangeMaxPoints - 1) / promRangeMaxPoints
		return fmt.Errorf("step %ds over a %ds window exceeds %d points per series; set step to at least %d seconds or narrow the window", step, window, promRangeMaxPoints, minStep)
	}
	return nil
}
