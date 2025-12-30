package change_events

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Integration test for get_change_events tool
func TestGetChangeEventsHandler_Integration(t *testing.T) {
	cfg := utils.SetupTestConfigOrSkip(t)

	handler := NewGetChangeEventsHandler(http.DefaultClient, *cfg)

	tests := []struct {
		name string
		args GetChangeEventsArgs
	}{
		{
			name: "Get change events with default parameters",
			args: GetChangeEventsArgs{
				LookbackMinutes: 60,
			},
		},
		{
			name: "Get change events with service filter",
			args: GetChangeEventsArgs{
				LookbackMinutes: 30,
				Service:         "test-service",
			},
		},
		{
			name: "Get change events with environment filter",
			args: GetChangeEventsArgs{
				LookbackMinutes: 60,
				Environment:     "prod",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			req := &mcp.CallToolRequest{}
			result, _, err := handler(ctx, req, tt.args)

			if utils.CheckAPIError(t, err) {
				return
			}

			text := utils.GetTextContent(t, result)

			var response map[string]interface{}
			if err := json.Unmarshal([]byte(text), &response); err != nil {
				t.Fatalf("failed to unmarshal response: %v", err)
			}

			count := 0
			if changeEvents, ok := response["change_events"].([]interface{}); ok {
				count = len(changeEvents)
			}
			availableEventNames := 0
			if eventNames, ok := response["available_event_names"].([]interface{}); ok {
				availableEventNames = len(eventNames)
			}
			t.Logf("Integration test successful: %d change event(s), %d available event name(s)",
				count, availableEventNames)
		})
	}
}
