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

const DiscoverSystemComponentsDescription = `
	Discover system components and topology information from Last9.
	This is a discovery tool to help users understand their system architecture.

	Use this tool when users ask questions like:
	- How many services are running in my system?
	- What is the topology of my deployment?
	- Show me the service map of my system
	- What pods are running?
	- List all namespaces
	- What containers are in pod X?
	- Show relationships between services
	- What components exist in my cluster?

	This tool takes no parameters. It returns a JSON with:
	- components: Map of component types (POD, SERVICE, CONTAINER, NAMESPACE, NODE, etc.) to arrays of names
	- metrics: Array of available metrics
	- triples: Array of relationships between components with src, rel, dst fields

	The relationships (triples) show how components are connected:
	- CONTAINS: A component contains another (e.g., POD contains CONTAINER)
	- RUNS: A node runs a pod
	- HAS_ENDPOINTS: A service has endpoints
	- HAS_INIT_CONTAINER: A pod has an init container
`

// DiscoverSystemComponentsArgs - empty struct as this tool takes no parameters
type DiscoverSystemComponentsArgs struct{}

func DiscoverSystemComponentsHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, DiscoverSystemComponentsArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args DiscoverSystemComponentsArgs) (*mcp.CallToolResult, any, error) {
		// Build the URL for system discovery API
		finalURL := fmt.Sprintf("%s%s?max_edges=80", cfg.APIBaseURL, constants.EndpointDiscoverSystem)

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
