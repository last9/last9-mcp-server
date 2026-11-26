package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"last9-mcp/internal/telemetry/traces"

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
	fixture := registeredToolFixture{
		tool:    &mcp.Tool{Name: "get_traces", Annotations: readOnlyTool("Get Traces"), InputSchema: traces.GetTracesInputSchema()},
		timeout: 10 * time.Second,
	}
	heldSession := connectToolFixture(t, fixture, traces.NewGetTracesHandler(upstream.Client(), cfg))
	heldCtx, cancelHeld := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelHeld()
	done := make(chan error, 3)
	for range 3 {
		go func() {
			res, err := heldSession.CallTool(heldCtx, &mcp.CallToolParams{Name: "get_traces", Arguments: args()})
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
	for _, scenario := range []string{"deadline", "cancellation", "invalid start", "invalid end"} {
		t.Run(scenario, func(t *testing.T) {
			fixture.timeout = 100 * time.Millisecond
			fixture.middleware = nil
			in := args()
			want := toolErrorExpectation{
				contains: []string{"get_traces is busy", "3 concurrent get_traces calls", "Retry shortly", "context deadline exceeded", "tool-call deadline", "Narrow the time range"},
				system:   true,
			}
			switch scenario {
			case "cancellation":
				fixture.middleware = cancelToolCall
				want = toolErrorExpectation{
					contains: []string{"get_traces is busy", "Retry shortly", "context canceled"},
					absent:   []string{"tool-call deadline", "Narrow the time range"},
				}
			case "invalid start", "invalid end":
				if scenario == "invalid start" {
					in.StartTimeISO = "not-a-time"
				} else {
					in.EndTimeISO = "not-a-time"
				}
				want = toolErrorExpectation{
					contains: []string{"failed to parse time range", "not-a-time"},
					absent:   []string{"get_traces is busy", "tool-call deadline", "Retry shortly"},
				}
			}
			session := connectToolFixture(t, fixture, traces.NewGetTracesHandler(upstream.Client(), cfg))
			result := callRegisteredTool(t, session, &mcp.CallToolParams{Name: "get_traces", Arguments: in})
			assertToolError(t, result, want)
			if got := arrived.Load(); got != 3 {
				t.Fatalf("upstream requests=%d, want only the 3 held calls", got)
			}
		})
	}
}

// Cancel inside receiving middleware: the SDK detaches handlers from the serve context.
func cancelToolCall(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		if method == "tools/call" {
			var cancel context.CancelFunc
			ctx, cancel = context.WithCancel(ctx)
			cancel()
		}
		return next(ctx, method, req)
	}
}
