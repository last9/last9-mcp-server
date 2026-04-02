package suggest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"last9-mcp/internal/constants"
	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const DidYouMeanDescription = `Suggests correct entity names when you're unsure of the exact spelling.

Use this tool PROACTIVELY before querying with an entity name you're not confident about — for example,
when the user types something that looks like a typo, abbreviation, or partial name.

Returns up to 3 closest matches with similarity scores (0–100%) from the Last9 catalog,
covering all entity types: services, environments, hosts, databases, k8s deployments,
k8s namespaces, jobs, and more.

When to use:
- Before calling get_service_logs, get_service_traces, get_service_performance_details, etc.
  with a service name that might be misspelled (e.g. "paymnt-svc", "prod-srvice")
- When a previous tool call returned empty results for a given entity name
- When the user says something ambiguous like "the payment thing" or "prod env"

Parameters:
- query: (Required) The name to search for — can be a partial name, misspelling, or abbreviation
- type: (Optional) Restrict suggestions to a specific entity type (e.g. "service", "environment",
  "host", "k8s_deployment", "k8s_namespace", "database", "job")

Examples:
- query="paymnt-svc"  → "payment-service (92%, service)"
- query="prod"        → "production (89%, environment)", "prod-eu (82%, environment)"
- query="payment"     → "payment-service (81%, service)", "payment-gateway (76%, service)"
- query="web-01"      → "web-01.prod (88%, host)"
- query="order-svc"   → "order-service (85%, k8s_deployment)"
`

// DidYouMeanArgs are the input parameters for the did_you_mean tool.
type DidYouMeanArgs struct {
	Query string `json:"query" jsonschema:"The misspelled or uncertain entity name to find suggestions for (required)"`
	Type  string `json:"type,omitempty" jsonschema:"Optional entity type filter: service, environment, host, database, k8s_deployment, k8s_namespace, job"`
}

type suggestionItem struct {
	Name  string  `json:"name"`
	Type  string  `json:"type"`
	Score float32 `json:"score"`
}

type suggestAPIResponse struct {
	Suggestions []suggestionItem `json:"suggestions"`
	Query       string           `json:"query"`
}

// suggestRequest is the POST body sent to the API's /suggest endpoint.
type suggestRequest struct {
	Query    string `json:"q"`
	Type     string `json:"type,omitempty"`
	ReadURL  string `json:"read_url,omitempty"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

// NewDidYouMeanHandler returns the handler for the did_you_mean MCP tool.
func NewDidYouMeanHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, DidYouMeanArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args DidYouMeanArgs) (*mcp.CallToolResult, any, error) {
		query := strings.TrimSpace(args.Query)
		if query == "" {
			return nil, nil, fmt.Errorf("query parameter is required")
		}

		payload := suggestRequest{
			Query:    query,
			ReadURL:  cfg.PrometheusReadURL,
			Username: cfg.PrometheusUsername,
			Password: cfg.PrometheusPassword,
		}
		if t := strings.TrimSpace(args.Type); t != "" {
			payload.Type = t
		}

		bodyBytes, err := json.Marshal(payload)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to marshal request: %w", err)
		}

		apiURL := fmt.Sprintf("%s%s", cfg.APIBaseURL, constants.EndpointSuggest)
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(bodyBytes))
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create request: %w", err)
		}

		httpReq.Header.Set(constants.HeaderAccept, constants.HeaderAcceptJSON)
		httpReq.Header.Set(constants.HeaderContentType, constants.HeaderContentTypeJSON)
		httpReq.Header.Set(constants.HeaderXLast9APIToken, constants.BearerPrefix+cfg.TokenManager.GetAccessToken(ctx))

		resp, err := client.Do(httpReq)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to fetch suggestions: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return nil, nil, fmt.Errorf("suggest API returned status %d: %s", resp.StatusCode, string(body))
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to read response: %w", err)
		}

		var apiResp suggestAPIResponse
		if err := json.Unmarshal(body, &apiResp); err != nil {
			return nil, nil, fmt.Errorf("failed to parse response: %w", err)
		}

		if len(apiResp.Suggestions) == 0 {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					&mcp.TextContent{
						Text: fmt.Sprintf("No suggestions found for %q. The entity may not exist in the catalog, or try a different spelling.", query),
					},
				},
			}, nil, nil
		}

		var sb strings.Builder
		fmt.Fprintf(&sb, "Did you mean one of these for %q?\n\n", query)
		for _, s := range apiResp.Suggestions {
			fmt.Fprintf(&sb, "  - %s (%d%% match, type: %s)\n", s.Name, int(s.Score*100), s.Type)
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: sb.String(),
				},
			},
		}, nil, nil
	}
}
