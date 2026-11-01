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

type labelNameScope struct {
	query, label string
	window       labelWindow
}

type labelNameChoice struct {
	query, label string
	notes        []string
	refusal      *mcp.CallToolResult
}

func resolveEmptyLabelLookup(ctx context.Context, client *http.Client, cfg models.Config, in labelNameScope) (labelNameChoice, error) {
	choice := labelNameChoice{query: in.query, label: in.label}
	window := in.window
	if window.isDefault && isMetricScopedSelector(in.query) {
		window.start = window.end - sparseLookbackMinutes*60
	}
	metric, err := resolveEmptyMetric(ctx, client, cfg, metricNameScope{query: in.query, start: window.start, end: window.end})
	if err != nil {
		return choice, err
	}
	choice.query, choice.refusal = metric.query, metric.refusal
	if metric.note != "" {
		choice.notes = append(choice.notes, metric.note)
	}
	if choice.refusal != nil || in.label == "" || in.label == "__name__" {
		return choice, nil
	}
	if len(in.label) > 512 {
		return choice, fmt.Errorf("label name discovery: name exceeds 512 bytes")
	}
	names, err := prom.DiscoverLabelNames(ctx, last9api.NewClient(client, cfg), prom.GetAPMLabelsInput{Datasource: promDatasource(cfg), Metric: choice.query, Timestamp: window.end, Window: window.end - window.start})
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		return choice, fmt.Errorf("label name discovery failed: %w", err)
	}
	resolution := nameresolution.Resolve("label", in.label, nameresolution.Catalog{Names: names, Usable: true})
	choice.refusal = resolution.Result()
	if resolution.Status == "closest" {
		choice.label = resolution.Canonical
		choice.notes = append(choice.notes, resolution.Note())
	}
	return choice, nil
}

func emptyLabelResult(result *mcp.CallToolResult, field string) bool {
	if result == nil || result.IsError || len(result.Content) == 0 {
		return false
	}
	text, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		return false
	}
	raw := []byte(text.Text)
	if field == "" {
		return emptyPromResult(raw)
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || len(fields) != 1 {
		return false
	}
	return emptyPromResult(fields[field])
}

func (choice labelNameChoice) annotate(result *mcp.CallToolResult) *mcp.CallToolResult {
	if result != nil {
		for _, note := range choice.notes {
			result = withDatasourceNote(result, note)
		}
	}
	return result
}
