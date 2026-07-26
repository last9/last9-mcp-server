package main

import (
	"context"

	"last9-mcp/internal/prompts"

	last9mcp "github.com/last9/mcp-go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	resourceURILogjson     = "last9://reference/logjson"
	resourceURITracejson   = "last9://reference/tracejson"
	resourceURIServiceLogs = "last9://reference/service_logs"
	resourceURIMetrics     = "last9://reference/metrics"
)

// registerReferenceResources registers whale tool manuals as MCP resources.
// Always registered (not gated by toolsets) so a metrics-only session can still
// read last9://reference/logjson if needed.
func registerReferenceResources(server *last9mcp.Last9MCPServer) {
	refs := []struct {
		uri         string
		name        string
		description string
		body        string
	}{
		{resourceURILogjson, "logjson", "Full logjson pipeline reference for get_logs", prompts.LogjsonReference},
		{resourceURITracejson, "tracejson", "Full tracejson pipeline reference for get_traces", prompts.TracejsonReference},
		{resourceURIServiceLogs, "service_logs", "Extended guidance for get_service_logs", prompts.ServiceLogsReference},
		{resourceURIMetrics, "metrics", "Prometheus range-query usage guide for prometheus_range_query", prompts.MetricsReference},
	}

	handler := func(_ context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		for _, ref := range refs {
			if req.Params.URI == ref.uri {
				return &mcp.ReadResourceResult{
					Contents: []*mcp.ResourceContents{{
						URI:      ref.uri,
						MIMEType: "text/markdown",
						Text:     ref.body,
					}},
				}, nil
			}
		}
		return nil, mcp.ResourceNotFoundError(req.Params.URI)
	}

	for _, ref := range refs {
		server.Server.AddResource(&mcp.Resource{
			URI:         ref.uri,
			Name:        ref.name,
			Description: ref.description,
			MIMEType:    "text/markdown",
		}, handler)
	}
}
