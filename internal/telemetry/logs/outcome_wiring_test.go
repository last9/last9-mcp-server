package logs

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestGetServiceLogsRawWithoutStatusFilterSkipsDiscovery(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"query_result": streamsResult(nil)})
	}))
	t.Cleanup(srv.Close)

	_, _, err := NewGetServiceLogsHandler(srv.Client(), apiModeCfg(srv.URL))(context.Background(), &mcp.CallToolRequest{}, GetServiceLogsArgs{
		ServiceName: "api", ResponseMode: logResponseModeRaw,
		StartTimeISO: "2023-11-14T22:13:20Z", EndTimeISO: "2023-11-14T22:23:20Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("upstream requests = %d, want only the raw log query", got)
	}
}
