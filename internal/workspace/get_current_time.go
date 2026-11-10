package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type GetCurrentTimeArgs struct {
	Range    string `json:"range,omitempty" jsonschema:"Optional range to resolve to absolute bounds, e.g. last 2h, today, yesterday, since yesterday 9am IST, since monday 10:00, 2026-02-09T15:00:00Z to 2026-02-09T16:00:00Z"`
	Timezone string `json:"timezone,omitempty" jsonschema:"Optional zone for calendar words in range and for the local renderings: an IANA name (Asia/Kolkata) or an abbreviation (IST, UTC, PST, EST). Default UTC"`
}

// NewGetCurrentTimeHandler takes the clock so tests can pin now.
func NewGetCurrentTimeHandler(now func() time.Time) func(context.Context, *mcp.CallToolRequest, GetCurrentTimeArgs) (*mcp.CallToolResult, any, error) {
	return func(_ context.Context, _ *mcp.CallToolRequest, args GetCurrentTimeArgs) (*mcp.CallToolResult, any, error) {
		out, err := resolveCurrentTime(now(), args.Range, args.Timezone)
		if err != nil {
			return nil, nil, err
		}
		body, err := json.Marshal(out)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to encode current time: %w", err)
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: string(body)}},
		}, nil, nil
	}
}
