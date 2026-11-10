package main

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/models"
	"last9-mcp/internal/telemetry/logs"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestGetLogsChunkBudgetReturnsRewriteToolError(t *testing.T) {
	var requests atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_, _ = w.Write([]byte(`{"data":{"resultType":"streams","result":[]}}`))
	}))
	defer upstream.Close()
	cfg := models.Config{
		APIBaseURL:   upstream.URL,
		TokenManager: &auth.TokenManager{AccessToken: "test-token", ExpiresAt: time.Now().Add(time.Hour)},
	}
	session := connectToolFixture(t, registeredToolFixture{
		tool:    &mcp.Tool{Name: "get_logs", Annotations: readOnlyTool("Get Logs")},
		timeout: time.Second,
	}, logs.NewGetLogsHandler(upstream.Client(), cfg))
	result := callRegisteredTool(t, session, &mcp.CallToolParams{
		Name: "get_logs",
		Arguments: map[string]any{
			"logjson_query":    []map[string]any{{"type": "filter", "query": map[string]any{"$contains": []any{"Body", "error"}}}},
			"lookback_minutes": 48 * 60,
		},
	})
	assertToolError(t, result, toolErrorExpectation{
		contains: []string{"48 chunks", "maximum 24", "lookback_minutes", "start_time_iso", "end_time_iso", "Do not retry unchanged"},
	})
	if got := requests.Load(); got != 0 {
		t.Errorf("refused call sent %d requests", got)
	}
}
