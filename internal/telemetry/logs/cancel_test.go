package logs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const cancelReturnBound = 2 * time.Second

func TestGetLogsHandler_ChunkedReadCancelledMidwayStartsNoChunkAndReturnsCancelled(t *testing.T) {
	bodySearch := []map[string]interface{}{{
		"type":  "filter",
		"query": map[string]interface{}{"$contains": []interface{}{"Body", "error"}},
	}}
	start := time.Unix(0, 0).UTC()
	end := start.Add(24 * time.Hour)
	adaptive := utils.GetAdaptiveLoadingConfig(utils.AdaptiveLoadingInput{
		StartMs: start.UnixMilli(), EndMs: end.UnixMilli(), Pipeline: bodySearch,
	})
	chunks := utils.GetAdaptiveChunks(start.UnixMilli(), end.UnixMilli(), adaptive)
	if len(chunks) <= adaptive.MaxParallelChunks {
		t.Fatalf("fixture has %d chunks for %d slots; want chunks left queued", len(chunks), adaptive.MaxParallelChunks)
	}

	release := make(chan struct{})
	var cancelled atomic.Bool
	var startedAfterCancel atomic.Int32
	inFlight := make(chan struct{}, len(chunks))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if cancelled.Load() {
			startedAfterCancel.Add(1)
		}
		inFlight <- struct{}{}
		holdUntilCancelled(r, release)
	}))
	defer server.Close()
	defer close(release)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		for range adaptive.MaxParallelChunks {
			<-inFlight
		}
		cancelled.Store(true)
		cancel()
	}()

	type outcome struct {
		result *mcp.CallToolResult
		err    error
		at     time.Time
	}
	done := make(chan outcome, 1)
	go func() {
		result, _, err := NewGetLogsHandler(server.Client(), testLogsConfig(server.URL))(ctx, &mcp.CallToolRequest{}, GetLogsArgs{
			LogjsonQuery: bodySearch,
			StartTimeISO: start.Format(time.RFC3339),
			EndTimeISO:   end.Format(time.RFC3339),
		})
		done <- outcome{result, err, time.Now()}
	}()

	select {
	case got := <-done:
		if got.result != nil || !errors.Is(got.err, context.Canceled) {
			t.Fatalf("handler = (%v, %v), want a context.Canceled error", got.result, got.err)
		}
	case <-time.After(cancelReturnBound):
		t.Fatalf("handler still running %s after cancel", cancelReturnBound)
	}
	if n := startedAfterCancel.Load(); n != 0 {
		t.Fatalf("%d chunk requests started after cancel, want 0", n)
	}
}

func TestGetLogsHandler_APIModeWindowsCancelledAfterOneSucceedsReturnsCancelled(t *testing.T) {
	end := time.Unix(1_800_000_000, 0).UTC()
	start := end.Add(-30 * 24 * time.Hour)
	newestStart := end.Add(-logSearchWindowLimit).Unix()

	release := make(chan struct{})
	blocked := make(chan struct{}, 8)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if windowStart, _ := body["start"].(float64); int64(windowStart) == newestStart {
			_, _ = fmt.Fprintf(w, `{"query_result":{"status":"success","data":{"resultType":"streams","result":[{"stream":{},"values":[["%d","line"]]}]}}}`,
				newestStart*int64(time.Second))
			return
		}
		blocked <- struct{}{}
		holdUntilCancelled(r, release)
	}))
	defer server.Close()
	defer close(release)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		for range 3 {
			<-blocked
		}
		cancel()
	}()

	// The finished window counts once its body is closed, i.e. fully decoded.
	client := server.Client()
	client.Transport = signalOnBodyClose{next: client.Transport, closed: blocked}
	result, _, err := NewGetLogsHandler(client, apiModeCfg(server.URL))(ctx, &mcp.CallToolRequest{}, GetLogsArgs{
		LogjsonQuery: []map[string]interface{}{{"type": "filter", "query": map[string]interface{}{"$eq": []interface{}{"ServiceName", "checkout"}}}},
		StartTimeISO: start.Format(time.RFC3339),
		EndTimeISO:   end.Format(time.RFC3339),
	})
	if result != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("handler = (%v, %v), want a context.Canceled error instead of the finished window", result, err)
	}
}

// holdUntilCancelled drains the body so the server notices the client hanging
// up; release frees the handler if the code under test never does.
func holdUntilCancelled(r *http.Request, release <-chan struct{}) {
	_, _ = io.Copy(io.Discard, r.Body)
	select {
	case <-r.Context().Done():
	case <-release:
	}
}

type signalOnBodyClose struct {
	next   http.RoundTripper
	closed chan<- struct{}
}

func (s signalOnBodyClose) RoundTrip(r *http.Request) (*http.Response, error) {
	resp, err := s.next.RoundTrip(r)
	if err == nil {
		resp.Body = signalingBody{ReadCloser: resp.Body, closed: s.closed}
	}
	return resp, err
}

type signalingBody struct {
	io.ReadCloser
	closed chan<- struct{}
}

func (b signalingBody) Close() error {
	b.closed <- struct{}{}
	return b.ReadCloser.Close()
}
