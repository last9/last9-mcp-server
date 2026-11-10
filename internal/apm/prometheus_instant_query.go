package apm

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"last9-mcp/internal/last9api"
	"last9-mcp/internal/last9api/prom"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type PromqlInstantQueryArgs struct {
	Query           string  `json:"query" jsonschema:"(Required) PromQL query to execute"`
	TimeISO         string  `json:"time_iso,omitempty" jsonschema:"Evaluation time in RFC3339/ISO8601 format (e.g. 2024-06-01T12:00:00Z). If omitted, defaults to now or now-lookback_minutes."`
	LookbackMinutes float64 `json:"lookback_minutes,omitempty" jsonschema:"Number of minutes to look back from now when time_iso is omitted (default: 0, minimum: 1)."`
	Datasource      string  `json:"datasource,omitempty" jsonschema:"Datasource name; omitted uses the default, a near miss its closest match (named in a second content block)."`
}

func resolveInstantQueryTime(timeISO string, lookbackMinutes float64) (int64, error) {
	if timeISO != "" {
		_, endTime, err := utils.GetTimeRange(map[string]interface{}{
			"end_time_iso": timeISO,
		}, utils.DefaultLookbackMinutes)
		if err != nil {
			return 0, fmt.Errorf("invalid time_iso format: %w", err)
		}
		return endTime.Unix(), nil
	}

	if lookbackMinutes != 0 {
		startTime, _, err := utils.GetTimeRange(map[string]interface{}{
			"lookback_minutes": lookbackMinutes,
		}, utils.DefaultLookbackMinutes)
		if err != nil {
			return 0, err
		}
		return startTime.Unix(), nil
	}

	return time.Now().UTC().Unix(), nil
}

func NewPromqlInstantQueryHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, PromqlInstantQueryArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args PromqlInstantQueryArgs) (*mcp.CallToolResult, any, error) {
		query := args.Query
		if query == "" {
			return nil, nil, fmt.Errorf("query is required")
		}

		timeParam, err := resolveInstantQueryTime(args.TimeISO, args.LookbackMinutes)
		if err != nil {
			return nil, nil, err
		}

		choice, err := chooseDatasource(cfg, args.Datasource)
		if err != nil {
			return nil, nil, err
		}
		if choice.candidates != nil {
			return ambiguousDatasourceResult(args.Datasource, choice), nil, nil
		}
		raw, err := prom.QueryInstantRaw(ctx, last9api.NewClient(client, choice.cfg), prom.QueryInstantInput{
			Datasource: promDatasource(choice.cfg),
			Query:      query,
			Timestamp:  timeParam,
		})
		return promToolResult(raw, err, choice.note)
	}
}
