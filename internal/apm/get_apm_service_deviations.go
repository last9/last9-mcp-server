package apm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"last9-mcp/internal/deeplink"
	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type DeviationArgs struct {
	ServiceName      string  `json:"service_name,omitempty"`
	Env              string  `json:"env,omitempty"`
	Datasource       string  `json:"datasource,omitempty"`
	StartTimeISO     string  `json:"start_time_iso,omitempty"`
	EndTimeISO       string  `json:"end_time_iso,omitempty"`
	LookbackMinutes  float64 `json:"lookback_minutes,omitempty"`
	BaselineStartISO string  `json:"baseline_start_time_iso,omitempty"`
	BaselineEndISO   string  `json:"baseline_end_time_iso,omitempty"`
	MaxServices      int     `json:"max_services,omitempty"`
	MaxOperations    int     `json:"max_operations,omitempty"`
}

// NewAPMServiceDeviationsHandler compares bounded APM RED aggregates across equal windows.
func NewAPMServiceDeviationsHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, DeviationArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args DeviationArgs) (*mcp.CallToolResult, any, error) {
		choice, err := chooseDatasource(cfg, args.Datasource)
		if err != nil {
			return nil, nil, err
		}
		if choice.candidates != nil {
			return ambiguousDatasourceResult(args.Datasource, choice), nil, nil
		}
		// Composite callers retain exact lookup: their evidence cannot carry a
		// candidate-selection response or disclose a substituted datasource.
		choice.cfg.DatasourceName = choice.name
		args.Datasource = ""
		result, structured, err := newAPMServiceDeviationsHandler(client, choice.cfg, defaultDeviationHandlerDeps())(ctx, req, args)
		if err != nil {
			return nil, nil, err
		}
		return withDatasourceNote(result, choice.note), structured, nil
	}
}

func newAPMServiceDeviationsHandler(client *http.Client, baseCfg models.Config, deps deviationHandlerDeps) func(context.Context, *mcp.CallToolRequest, DeviationArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args DeviationArgs) (*mcp.CallToolResult, any, error) {
		result, err := runAPMServiceDeviations(ctx, client, baseCfg, deps, args)
		if err != nil {
			return nil, nil, err
		}
		payload, err := json.Marshal(result)
		if err != nil {
			return nil, nil, fmt.Errorf("marshal APM deviation response: %w", err)
		}
		return &mcp.CallToolResult{
			Meta:              deeplink.ToMeta(result.DashboardURL),
			Content:           []mcp.Content{&mcp.TextContent{Text: string(payload)}},
			StructuredContent: result,
		}, nil, nil
	}
}
