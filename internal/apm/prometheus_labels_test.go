package apm

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Integration test for prometheus_labels tool
func TestPromqlLabelsHandler_Integration(t *testing.T) {
	cfg := utils.SetupTestConfigOrSkip(t)

	handler := NewPromqlLabelsHandler(http.DefaultClient, *cfg)

	args := PromqlLabelsArgs{
		MatchQuery: "up",
	}

	ctx := context.Background()
	req := &mcp.CallToolRequest{}
	result, _, err := handler(ctx, req, args)

	if utils.CheckAPIError(t, err) {
		return
	}

	text := utils.GetTextContent(t, result)

	var labels []string
	if err := json.Unmarshal([]byte(text), &labels); err != nil {
		t.Logf("Integration test successful. Response is formatted text (not JSON)")
	} else {
		t.Logf("Integration test successful: found %d label(s)", len(labels))
	}
}
