package suggest

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"unicode"

	"last9-mcp/internal/constants"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	maxDidYouMeanSuggestions = 3
)

// DidYouMeanArgs are the input parameters for the did_you_mean tool.
type DidYouMeanArgs struct {
	Query string `json:"query" jsonschema:"(Required) The misspelled or uncertain entity name to find suggestions for"`
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

		if cfg.TokenManager == nil {
			return nil, nil, fmt.Errorf("token manager is not configured")
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
			return nil, nil, utils.NewUpstreamHTTPError(resp, "suggest API")
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

		suggestions := rankSuggestions(query, apiResp.Suggestions)
		var sb strings.Builder
		fmt.Fprintf(&sb, "Did you mean one of these for %q?\n\n", query)
		for _, s := range suggestions[:min(len(suggestions), maxDidYouMeanSuggestions)] {
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

// rankSuggestions scores a name made of the query's words in another order
// as a full match, since character similarity rates it no higher than
// unrelated names, then orders by score, shared words and name.
func rankSuggestions(query string, items []suggestionItem) []suggestionItem {
	queryWords := words(query)
	ranked := make([]suggestionItem, len(items))
	overlap := make(map[string]int, len(items))
	for i, s := range items {
		nameWords := words(s.Name)
		if len(queryWords) > 0 && slices.Equal(queryWords, nameWords) {
			s.Score = 1
		}
		overlap[s.Name] = sharedWords(queryWords, nameWords)
		ranked[i] = s
	}
	slices.SortStableFunc(ranked, func(a, b suggestionItem) int {
		return cmp.Or(
			cmp.Compare(b.Score, a.Score),
			cmp.Compare(overlap[b.Name], overlap[a.Name]),
			strings.Compare(a.Name, b.Name),
		)
	})
	return ranked
}

func words(name string) []string {
	w := strings.FieldsFunc(strings.ToLower(name), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	slices.Sort(w)
	return w
}

func sharedWords(a, b []string) int {
	n := 0
	for i, j := 0, 0; i < len(a) && j < len(b); {
		switch strings.Compare(a[i], b[j]) {
		case 0:
			n++
			i++
			j++
		case -1:
			i++
		default:
			j++
		}
	}
	return n
}
