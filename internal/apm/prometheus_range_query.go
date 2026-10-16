package apm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type PromqlRangeQueryArgs struct {
	Query           string  `json:"query" jsonschema:"(Required) PromQL query to execute"`
	StartTimeISO    string  `json:"start_time_iso,omitempty" jsonschema:"Start time in RFC3339/ISO8601 format (e.g. 2024-06-01T12:00:00Z). Optional when lookback_minutes is provided."`
	EndTimeISO      string  `json:"end_time_iso,omitempty" jsonschema:"End time in RFC3339/ISO8601 format (e.g. 2024-06-01T13:00:00Z). Defaults to now when omitted."`
	LookbackMinutes float64 `json:"lookback_minutes,omitempty" jsonschema:"Number of minutes to look back from now (default: 60, minimum: 1). Use for relative windows like last 30 minutes."`
	Datasource      string  `json:"datasource,omitempty" jsonschema:"Name of the datasource to query. If omitted, uses the default configured datasource."`
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

		queryCfg, err := resolveDatasourceCfg(cfg, args.Datasource)
		if err != nil {
			return nil, nil, err
		}

		httpResp, err := utils.MakePromRangeAPIQuery(ctx, client, query, startTimeParam, endTimeParam, queryCfg, utils.PromResolution{})
		if err != nil {
			return nil, nil, err
		}
		if httpResp == nil {
			return nil, nil, fmt.Errorf("received nil response from Prometheus")
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode != http.StatusOK {
			return promToolError(httpResp, "Prometheus range query")
		}
		responseBodyBytes, err := io.ReadAll(httpResp.Body)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to read response body: %w", err)
		}
		return promRangeQueryResult(responseBodyBytes), nil, nil
	}
}

// promRangeQueryResult returns the series array as the upstream sent it,
// trimming trailing series past MaxResultTextBytes. A body that is not a
// series array is passed through untouched.
func promRangeQueryResult(body []byte) *mcp.CallToolResult {
	var series []json.RawMessage
	if err := json.Unmarshal(body, &series); err != nil {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(body)}}}
	}
	var env utils.ResultEnvelope
	if len(series) == 0 {
		env.Hint("No series matched. Check the metric name with prometheus_label_values (label __name__) and label names and values with prometheus_labels and prometheus_label_values, or widen the window.")
	}
	text := string(body)
	if len(body) > utils.MaxResultTextBytes {
		kept, omitted := utils.FitItems(series, utils.MaxResultTextBytes, jsonArraySize)
		encoded, err := json.Marshal(kept)
		if err == nil {
			series, text = kept, string(encoded)
			env.Trim(omitted)
		}
	}
	return utils.EnvelopeToolResult(series, text, env)
}

func jsonArraySize(items []json.RawMessage) int {
	size := 2 + max(len(items)-1, 0)
	for _, item := range items {
		size += len(item)
	}
	return size
}
