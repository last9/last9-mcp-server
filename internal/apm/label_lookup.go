package apm

import (
	"errors"
	"fmt"
	"io"
	"net/http"

	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// labelLookup is one prometheus_labels or prometheus_label_values call; fetch runs it over [start, end].
type labelLookup struct {
	op    string
	fetch func(start, end int64) (*http.Response, error)
}

// run queries the caller's window.
func (l labelLookup) run(startTimeISO, endTimeISO string, lookbackMinutes float64) (*mcp.CallToolResult, any, error) {
	start, end, err := resolveTimeRange(startTimeISO, endTimeISO, lookbackMinutes)
	if err != nil {
		return nil, nil, err
	}
	body, err := l.read(start, end)
	var upstreamErr *utils.UpstreamHTTPError
	if errors.As(err, &upstreamErr) {
		return utils.ToolErrorResult(err.Error()), nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	return textResult(body), nil, nil
}

func (l labelLookup) read(start, end int64) ([]byte, error) {
	httpResp, err := l.fetch(start, end)
	if err != nil {
		return nil, err
	}
	if httpResp == nil {
		return nil, fmt.Errorf("received nil response from Prometheus")
	}
	defer httpResp.Body.Close()
	if httpResp.StatusCode != http.StatusOK {
		return nil, promErr(httpResp, l.op)
	}
	data, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}
	return data, nil
}

func textResult(body []byte, notes ...string) *mcp.CallToolResult {
	content := []mcp.Content{&mcp.TextContent{Text: string(body)}}
	for _, n := range notes {
		content = append(content, &mcp.TextContent{Text: n})
	}
	return &mcp.CallToolResult{Content: content}
}
