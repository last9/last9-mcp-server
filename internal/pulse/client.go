package pulse

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"last9-mcp/internal/constants"
	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	pulseBasePath     = "/pulse/alert-hygiene"
	maxResponseBytes  = 5 * 1024 * 1024
	maxErrorBodyBytes = 4096
	defaultPageLimit  = 25
	maximumPageLimit  = 100
)

type request struct {
	method string
	path   string
	query  url.Values
	body   any
}

type client struct {
	httpClient *http.Client
	config     models.Config
}

// APIError preserves the upstream status and bounded response body.
type APIError struct {
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("Pulse API returned status %d: %s", e.StatusCode, e.Body)
}

func newClient(httpClient *http.Client, config models.Config) *client {
	return &client{httpClient: httpClient, config: config}
}

func (c *client) call(ctx context.Context, input request) ([]byte, error) {
	payload, err := marshalBody(input.body)
	if err != nil {
		return nil, err
	}
	req, err := c.newRequest(ctx, input, payload)
	if err != nil {
		return nil, err
	}
	return c.do(req)
}

func marshalBody(body any) ([]byte, error) {
	if body == nil {
		return nil, nil
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("encode Pulse request: %w", err)
	}
	return payload, nil
}

func (c *client) newRequest(ctx context.Context, input request, payload []byte) (*http.Request, error) {
	endpoint := c.config.APIBaseURL + pulseBasePath + input.path
	if len(input.query) > 0 {
		endpoint += "?" + input.query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, input.method, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create Pulse request: %w", err)
	}
	c.setHeaders(req, len(payload) > 0)
	return req, nil
}

func (c *client) setHeaders(req *http.Request, hasBody bool) {
	token := c.config.TokenManager.GetAccessToken(req.Context())
	req.Header.Set(constants.HeaderAccept, constants.HeaderAcceptJSON)
	req.Header.Set(constants.HeaderXLast9APIToken, constants.BearerPrefix+token)
	req.Header.Set(constants.HeaderUserAgent, constants.UserAgentLast9MCP)
	if hasBody {
		req.Header.Set(constants.HeaderContentType, constants.HeaderContentTypeJSON)
	}
}

func (c *client) do(req *http.Request) ([]byte, error) {
	response, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Pulse API request failed: %w", err)
	}
	defer response.Body.Close()
	body, err := readBounded(response.Body)
	if err != nil {
		return nil, err
	}
	if response.StatusCode >= http.StatusBadRequest {
		return nil, apiError(response.StatusCode, body)
	}
	return body, nil
}

func readBounded(reader io.Reader) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(reader, maxResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read Pulse API response: %w", err)
	}
	if len(body) > maxResponseBytes {
		return nil, fmt.Errorf("Pulse API response exceeds %d bytes; narrow the page", maxResponseBytes)
	}
	return body, nil
}

func apiError(status int, body []byte) error {
	message := strings.TrimSpace(string(body))
	if len(message) > maxErrorBodyBytes {
		message = message[:maxErrorBodyBytes] + "..."
	}
	if message == "" {
		message = http.StatusText(status)
	}
	return &APIError{StatusCode: status, Body: message}
}

func textResult(body []byte) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(body)}}}
}

func escapedID(value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("ID is required")
	}
	if strings.ContainsAny(value, "/?#") || value == "." || value == ".." {
		return "", fmt.Errorf("ID contains invalid path characters")
	}
	return url.PathEscape(value), nil
}

func pageQuery(limit int, cursor string) (url.Values, error) {
	if limit == 0 {
		limit = defaultPageLimit
	}
	if limit < 1 || limit > maximumPageLimit {
		return nil, fmt.Errorf("limit must be between 1 and 100")
	}
	query := url.Values{"limit": []string{fmt.Sprintf("%d", limit)}}
	if cursor != "" {
		query.Set("cursor", cursor)
	}
	return query, nil
}
