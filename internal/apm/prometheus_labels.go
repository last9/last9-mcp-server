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

type PromqlLabelsArgs struct {
	MatchQuery      string  `json:"match_query,omitempty" jsonschema:"PromQL query to match series (e.g. up{job=\"prometheus\"})"`
	Match           string  `json:"match,omitempty" jsonschema:"Alias of match_query (matches the Prometheus API's match parameter); ignored when match_query is set."`
	StartTimeISO    string  `json:"start_time_iso,omitempty" jsonschema:"Start time in RFC3339/ISO8601 format (e.g. 2024-06-01T12:00:00Z). Optional when lookback_minutes is provided."`
	EndTimeISO      string  `json:"end_time_iso,omitempty" jsonschema:"End time in RFC3339/ISO8601 format (e.g. 2024-06-01T13:00:00Z). Defaults to now when omitted."`
	LookbackMinutes float64 `json:"lookback_minutes,omitempty" jsonschema:"Number of minutes to look back from now (default: 60, minimum: 1). When omitted with no ISO bounds, an empty result for a single-metric match_query is retried once over 5760 (4 days)."`
	Datasource      string  `json:"datasource,omitempty" jsonschema:"Datasource name; omitted uses the default, a near miss its closest match (named in an additional content block)."`
}

func NewPromqlLabelsHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, PromqlLabelsArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args PromqlLabelsArgs) (*mcp.CallToolResult, any, error) {
		query := firstNonEmpty(args.MatchQuery, args.Match)
		if query == "" {
			return nil, nil, fmt.Errorf("match_query is required")
		}
		window, err := resolveLabelWindow(args.StartTimeISO, args.EndTimeISO, args.LookbackMinutes)
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
		api := last9api.NewClient(client, choice.cfg)
		lookup := labelLookup{
			noun:       "labels",
			field:      "labels",
			matchQuery: query,
			fetch: func(start, end int64) ([]byte, error) {
				return prom.GetAPMLabels(ctx, api, prom.GetAPMLabelsInput{
					Datasource: promDatasource(choice.cfg),
					Metric:     query,
					Timestamp:  end,
					Window:     end - start,
				})
			},
		}
		return lookup.run(window, choice.note)
	}
}
