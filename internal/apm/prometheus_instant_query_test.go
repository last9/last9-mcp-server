package apm

import (
	"context"
	"net/http"
	"testing"
	"time"

	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestPromqlInstantQueryHandler(t *testing.T) {
	cfg := utils.SetupTestConfigOrSkip(t)

	handler := NewPromqlInstantQueryHandler(http.DefaultClient, *cfg)

	args := PromqlInstantQueryArgs{
		Query:   "sum_over_time(trace_call_graph_count{}[1h])",
		TimeISO: time.Now().UTC().Format(time.RFC3339),
	}

	ctx := context.Background()
	req := &mcp.CallToolRequest{}
	result, _, err := handler(ctx, req, args)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	_ = utils.GetTextContent(t, result)
}

func TestResolveInstantQueryTime(t *testing.T) {
	timeParam, err := resolveInstantQueryTime("2025-06-23T16:00:00Z", 30)
	if err != nil {
		t.Fatalf("resolveInstantQueryTime() returned error: %v", err)
	}
	if timeParam != 1750694400 {
		t.Fatalf("timeParam = %d, want %d", timeParam, int64(1750694400))
	}

	timeParam, err = resolveInstantQueryTime("", 30)
	if err != nil {
		t.Fatalf("resolveInstantQueryTime() lookback returned error: %v", err)
	}
	expected := time.Now().UTC().Add(-30 * time.Minute).Unix()
	if timeParam < expected-5 || timeParam > expected+5 {
		t.Fatalf("lookback timeParam = %d, expected near %d", timeParam, expected)
	}
}
