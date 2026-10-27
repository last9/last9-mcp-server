package logs

import (
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const maxRawResultBytes = 32000

// The SDK adds resultType after the handler for protocol 2026-07-28 and later.
const sdkResultTypeBytes = len(`,"resultType":"complete"`)

func boundRawResult(full *mcp.CallToolResult, count int, info *mcp.Implementation, prefix func(int) (*mcp.CallToolResult, error)) (*mcp.CallToolResult, error) {
	size := func(result *mcp.CallToolResult) (int, error) {
		projected := *result
		if info != nil {
			projected.Meta = mapsClone(result.Meta)
			if _, ok := projected.Meta[mcp.MetaKeyServerInfo]; !ok {
				projected.Meta[mcp.MetaKeyServerInfo] = info
			}
		}
		encoded, err := json.Marshal(&projected)
		return len(encoded) + sdkResultTypeBytes, err
	}
	n, err := size(full)
	if err != nil {
		return nil, err
	}
	if n <= maxRawResultBytes {
		return full, nil
	}
	var best *mcp.CallToolResult
	for low, high := 1, count; low <= high; {
		mid := low + (high-low)/2
		candidate, err := prefix(mid)
		if err != nil {
			return nil, err
		}
		n, err := size(candidate)
		if err != nil {
			return nil, err
		}
		if n <= maxRawResultBytes {
			best = candidate
			low = mid + 1
		} else {
			high = mid - 1
		}
	}
	if best != nil {
		return best, nil
	}
	fallback := &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: "No log entries returned: the response exceeds the 32000-byte limit. Narrow the filters or time range, use summary mode, or open logs in the UI."}}}
	n, err = size(fallback)
	if err != nil {
		return nil, err
	}
	if n > maxRawResultBytes {
		return nil, fmt.Errorf("server metadata exceeds the 32000-byte log response limit")
	}
	return fallback, nil
}

func rawLimitHint(returned, fetched int) string {
	return fmt.Sprintf("Response limited to 32000 bytes; returned %d of %d fetched log entries. Narrow the filters or time range, or open logs in the UI.", returned, fetched)
}
