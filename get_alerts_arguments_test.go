package main

import (
	"context"
	"encoding/json"
	"testing"

	"last9-mcp/internal/alerting"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestGetAlertsRejectsNonIntegerLimitsBeforeHandler(t *testing.T) {
	var calls int
	handler := func(_ context.Context, _ *mcp.CallToolRequest, _ alerting.GetAlertsArgs) (*mcp.CallToolResult, any, error) {
		calls++
		return &mcp.CallToolResult{}, nil, nil
	}
	session := connectToolFixture(t, registeredToolFixture{
		tool: &mcp.Tool{Name: "get_alerts", Description: "x", InputSchema: alerting.GetAlertsInputSchema()},
	}, handler)

	for _, args := range []string{`{"limit":"3"}`, `{"limit":null}`} {
		result := callRegisteredTool(t, session, &mcp.CallToolParams{
			Name:      "get_alerts",
			Arguments: json.RawMessage(args),
		})
		if !result.IsError {
			t.Errorf("arguments %s: want validation error", args)
		}
	}
	if calls != 0 {
		t.Fatalf("handler calls = %d, want 0", calls)
	}
}
