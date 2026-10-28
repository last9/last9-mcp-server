package traces

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func oneChunkTracesArgs() GetTracesArgs {
	return GetTracesArgs{
		TracejsonQuery: []map[string]interface{}{
			{
				"type":  "filter",
				"query": map[string]interface{}{"$neq": []interface{}{"ServiceName", ""}},
			},
		},
		StartTimeISO: "1970-01-01T00:00:00Z",
		EndTimeISO:   "1970-01-01T00:30:00Z",
		Limit:        1,
	}
}

func TestGetTracesWaitsForAFreeSlotBeyondTheConcurrencyCap(t *testing.T) {
	var arrived atomic.Int32
	arrivals := make(chan struct{}, maxConcurrentGetTraces+1)
	release := make(chan struct{})
	releaseHeld := sync.OnceFunc(func() { close(release) })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		arrived.Add(1)
		arrivals <- struct{}{}
		<-release
		_, _ = w.Write([]byte(traceAPIResponse(1)))
	}))
	defer server.Close()
	defer releaseHeld()
	handler := NewGetTracesHandler(server.Client(), testChunkTracesConfig(server.URL))

	var held sync.WaitGroup
	heldErrs := make(chan error, maxConcurrentGetTraces)
	for range maxConcurrentGetTraces {
		held.Go(func() {
			result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, oneChunkTracesArgs())
			if err == nil && result.IsError {
				err = errors.New(resultText(result))
			}
			heldErrs <- err
		})
	}
	for range maxConcurrentGetTraces {
		select {
		case <-arrivals:
		case <-time.After(5 * time.Second):
			t.Fatalf("only %d of %d held calls reached upstream", arrived.Load(), maxConcurrentGetTraces)
		}
	}
	for _, tt := range []struct {
		name string
		args GetTracesArgs
	}{
		{name: "start_time_iso", args: invalidTraceTimeArgs("start")},
		{name: "end_time_iso", args: invalidTraceTimeArgs("end")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
			defer cancel()
			_, _, err := handler(ctx, &mcp.CallToolRequest{}, tt.args)
			if err == nil || !strings.Contains(err.Error(), "failed to parse time range") {
				t.Fatalf("expected immediate time validation error, got %v", err)
			}
			if ctx.Err() != nil {
				t.Fatalf("invalid bounds waited for a slot: %v", ctx.Err())
			}
			if got := arrived.Load(); got != maxConcurrentGetTraces {
				t.Fatalf("invalid bounds reached upstream: got %d requests, want %d", got, maxConcurrentGetTraces)
			}
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	result, _, err := handler(ctx, &mcp.CallToolRequest{}, oneChunkTracesArgs())
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if got := arrived.Load(); got != maxConcurrentGetTraces {
		t.Fatalf("upstream saw %d calls, want %d: the call over the cap must not fetch", got, maxConcurrentGetTraces)
	}
	if !result.IsError {
		t.Fatalf("expected an error result for the call over the cap, got %+v", result)
	}
	if text := resultText(result); !strings.Contains(text, "concurrent get_traces calls") {
		t.Fatalf("busy error should name the concurrency cap, got %q", text)
	}

	releaseHeld()
	held.Wait()
	close(heldErrs)
	for err := range heldErrs {
		if err != nil {
			t.Fatalf("held call failed: %v", err)
		}
	}

	if result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, oneChunkTracesArgs()); err != nil || result.IsError {
		t.Fatalf("a call after the held calls finished must get a slot, got err=%v result=%+v", err, result)
	}
}

func invalidTraceTimeArgs(bound string) GetTracesArgs {
	args := oneChunkTracesArgs()
	if bound == "start" {
		args.StartTimeISO = "not-a-time"
	} else {
		args.EndTimeISO = "not-a-time"
	}
	return args
}

func resultText(result *mcp.CallToolResult) string {
	var text strings.Builder
	for _, content := range result.Content {
		if tc, ok := content.(*mcp.TextContent); ok {
			text.WriteString(tc.Text)
		}
	}
	return text.String()
}
