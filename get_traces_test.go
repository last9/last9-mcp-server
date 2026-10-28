package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"last9-mcp/internal/telemetry/traces"
	"last9-mcp/internal/toolsets"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestGetTracesRegisteredWaiterReportsBusy(t *testing.T) {
	var arrived atomic.Int32
	arrivals := make(chan struct{}, 4)
	release := make(chan struct{})
	releaseAll := sync.OnceFunc(func() { close(release) })
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		arrived.Add(1)
		arrivals <- struct{}{}
		<-release
		_, _ = fmt.Fprint(w, `{"data":{"result":[]}}`)
	}))
	defer upstream.Close()
	defer releaseAll()
	cfg := writeToolTestConfig(upstream.URL)
	args := func() traces.GetTracesArgs {
		return traces.GetTracesArgs{
			TracejsonQuery: []map[string]interface{}{{"type": "filter", "query": map[string]interface{}{"$neq": []interface{}{"ServiceName", ""}}}},
			StartTimeISO:   "1970-01-01T00:00:00Z",
			EndTimeISO:     "1970-01-01T00:30:00Z",
			Limit:          1,
		}
	}
	done := make(chan error, 3)
	for range 3 {
		handler := traces.NewGetTracesHandler(upstream.Client(), cfg)
		go func() {
			res, _, err := handler(context.Background(), &mcp.CallToolRequest{}, args())
			if err == nil && res.IsError {
				err = fmt.Errorf("held call: %v", res)
			}
			done <- err
		}()
	}
	defer func() {
		releaseAll()
		for range 3 {
			if err := <-done; err != nil {
				t.Error(err)
			}
		}
	}()
	for range 3 {
		select {
		case <-arrivals:
		case <-time.After(3 * time.Second):
			t.Fatal("held calls did not arrive")
		}
	}
	server := newRegisterTestServer(t)
	gate := newToolGate(toolsets.Filter{})
	gate.callTimeout = 100 * time.Millisecond
	tool := &mcp.Tool{Name: "get_traces", Annotations: readOnlyTool("Get Traces"), InputSchema: traces.GetTracesInputSchema()}
	if err := registerIfAllowed(server, gate, tool, traces.NewGetTracesHandler(upstream.Client(), cfg)); err != nil {
		t.Fatal(err)
	}
	if err := gate.registerAdmitted(); err != nil {
		t.Fatal(err)
	}
	session := connectRegisteredServer(t, server)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "get_traces", Arguments: args()})
	if err != nil {
		t.Fatal(err)
	}
	if got := arrived.Load(); got != 3 {
		t.Fatalf("upstream requests=%d, want 3", got)
	}
	if text := toolResultText(result); !result.IsError || !strings.Contains(text, "get_traces is busy") || !strings.Contains(text, "Retry shortly") {
		t.Fatalf("want busy isError from registered waiter, got isError=%v text=%q", result.IsError, text)
	}
	if result.Meta[utils.ErrorTypeMetaKey] != utils.SystemErrorType {
		t.Fatalf("deadline error missing system_error tag: %v", result.Meta)
	}
}
