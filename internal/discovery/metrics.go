package discovery

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"last9-mcp/internal/constants"
	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const DiscoverMetricsDescription = `
	Discover available metrics and their labels from Last9.
	This is a discovery tool to help users find the right metrics for their queries.

	Use this tool when users ask questions like:
	- What metrics are available?
	- Show me metrics related to CPU/memory/network
	- What labels does metric X have?
	- Help me find the right metric for monitoring Y
	- List all available metrics and their labels
	- What metrics can I use to monitor my services?

	This tool takes no parameters. It returns a JSON with available metrics and their associated labels/metadata to help users construct the right queries.
`

// DiscoverMetricsArgs - empty struct as this tool takes no parameters
type DiscoverMetricsArgs struct{}

func DiscoverMetricsHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, DiscoverMetricsArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args DiscoverMetricsArgs) (*mcp.CallToolResult, any, error) {
		// Build the URL for metrics discovery API
		finalURL := fmt.Sprintf("%s%s?max_edges=80", cfg.APIBaseURL, constants.EndpointDiscoverMetrics)

		// Create HTTP request
		httpReq, err := http.NewRequestWithContext(ctx, "GET", finalURL, nil)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create request: %w", err)
		}

		// Set headers
		httpReq.Header.Set(constants.HeaderAccept, constants.HeaderAcceptJSON)
		httpReq.Header.Set(constants.HeaderContentType, constants.HeaderContentTypeJSON)
		httpReq.Header.Set(constants.HeaderXLast9APIToken, constants.BearerPrefix+cfg.TokenManager.GetAccessToken(ctx))

		// Make the request
		resp, err := client.Do(httpReq)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to make request: %w", err)
		}
		defer resp.Body.Close()

		// Check response status
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return nil, nil, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
		}

		// Read response body
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to read response: %w", err)
		}

		// Return raw JSON response as-is
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: string(body),
				},
			},
		}, nil, nil
	}
}
