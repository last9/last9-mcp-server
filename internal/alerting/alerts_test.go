package alerting

import (
	"context"
	"net/http"
	"testing"

	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Integration test for get_alert_config tool
func TestGetAlertConfigHandler_Integration(t *testing.T) {
	cfg := utils.SetupTestConfigOrSkip(t)

	handler := NewGetAlertConfigHandler(http.DefaultClient, *cfg)

	tests := []struct {
		name string
		args GetAlertConfigArgs
	}{
		{
			name: "unfiltered",
			args: GetAlertConfigArgs{},
		},
		{
			name: "filtered by rule type",
			args: GetAlertConfigArgs{
				RuleType: "static",
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
			if text == "" {
				t.Fatal("expected non-empty response")
			}

			t.Logf("Integration test successful for %s", tt.name)
		})
	}
}
