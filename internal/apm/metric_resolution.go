package apm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"last9-mcp/internal/last9api"
	"last9-mcp/internal/last9api/prom"
	"last9-mcp/internal/models"
	"last9-mcp/internal/nameresolution"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type metricNameScope struct {
	query      string
	start, end int64
}

type metricNameChoice struct {
	query, note string
	refusal     *mcp.CallToolResult
}

func resolveEmptyMetric(ctx context.Context, client *http.Client, cfg models.Config, in metricNameScope) (metricNameChoice, error) {
	choice := metricNameChoice{query: in.query}
	selector, ok := nameresolution.ParseMetricSelector(in.query)
	if !ok || cfg.PrometheusReadURL == "" || len(selector.Name) < 3 {
		return choice, nil
	}
	if len(selector.Name) > 512 {
		return choice, fmt.Errorf("metric name discovery: name exceeds 512 bytes")
	}
	names, err := prom.DiscoverLabelValues(ctx, last9api.NewClient(client, cfg), prom.GetLabelValuesInput{Datasource: promDatasource(cfg), Label: "__name__", Matches: []string{selector.Scope()}, Timestamp: in.end, Window: in.end - in.start})
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		return choice, fmt.Errorf("metric name discovery failed: %w", err)
	}
	resolution := nameresolution.Resolve("metric", selector.Name, nameresolution.Catalog{Names: names, Usable: true})
	choice.refusal = resolution.Result()
	choice.note = resolution.Note()
	if resolution.Status == "closest" {
		choice.query = selector.WithName(resolution.Canonical)
	}
	return choice, nil
}

func emptyPromResult(raw []byte) bool {
	var values []json.RawMessage
	return json.Unmarshal(raw, &values) == nil && values != nil && len(values) == 0
}
