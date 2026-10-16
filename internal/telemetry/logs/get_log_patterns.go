package logs

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"

	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// GetLogPatternsArgs is the input of the get_log_patterns tool.
type GetLogPatternsArgs struct {
	LogPatternScopeArgs
	TopN int `json:"top_n,omitempty" jsonschema:"Patterns returned by count before the rest fold into other (default: 20, maximum: 100)"`
}

// GetLogPatternsResponse is the output of the get_log_patterns tool.
type GetLogPatternsResponse struct {
	Service       string              `json:"service"`
	Env           string              `json:"env,omitempty"`
	Window        LogPatternWindow    `json:"window"`
	TotalPatterns int                 `json:"total_patterns"`
	Patterns      []LogPatternSummary `json:"patterns"`
	Other         *LogPatternOther    `json:"other,omitempty"`
}

// LogPatternSummary is one template and its footprint in the sample.
type LogPatternSummary struct {
	Template string `json:"template"`
	LogPatternStats
	Sample string `json:"sample"`
}

// LogPatternOther totals the patterns that fell below top_n.
type LogPatternOther struct {
	Patterns int `json:"patterns"`
	Count    int `json:"count"`
}

// NewGetLogPatternsHandler creates the handler for the get_log_patterns tool.
func NewGetLogPatternsHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, GetLogPatternsArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args GetLogPatternsArgs) (*mcp.CallToolResult, any, error) {
		scope, err := newLogPatternScope(args.LogPatternScopeArgs)
		if err != nil {
			return nil, nil, err
		}
		topN, err := boundedPatternLimit("top_n", args.TopN, defaultPatternTopN, maxPatternTopN)
		if err != nil {
			return nil, nil, err
		}
		start, end, err := args.timeRange()
		if err != nil {
			return nil, nil, err
		}

		lines, window, err := scope.sample(ctx, client, cfg, start, end)
		if err != nil {
			return nil, nil, err
		}
		miner := newLogPatternMiner(1)
		for _, line := range lines {
			miner.add(0, line)
		}

		response := summariseLogPatterns(miner.patterns, topN)
		response.Service = scope.service
		response.Env = scope.env
		response.Window = window

		text, err := json.MarshalIndent(response, "", "  ")
		if err != nil {
			return nil, nil, fmt.Errorf("failed to format response: %w", err)
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(text)}}}, nil, nil
	}
}

func summariseLogPatterns(patterns []*logPattern, topN int) GetLogPatternsResponse {
	summaries := make([]LogPatternSummary, 0, len(patterns))
	for _, p := range patterns {
		summaries = append(summaries, LogPatternSummary{Template: p.template(), LogPatternStats: p.stats(0), Sample: p.sample})
	}
	sort.SliceStable(summaries, func(i, j int) bool {
		if summaries[i].Count != summaries[j].Count {
			return summaries[i].Count > summaries[j].Count
		}
		return summaries[i].Template < summaries[j].Template
	})

	response := GetLogPatternsResponse{TotalPatterns: len(summaries), Patterns: summaries}
	if len(summaries) > topN {
		other := &LogPatternOther{Patterns: len(summaries) - topN}
		for _, s := range summaries[topN:] {
			other.Count += s.Count
		}
		response.Patterns, response.Other = summaries[:topN], other
	}
	return response
}
