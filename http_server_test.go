package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestStatelessStreamableHandler verifies the HTTP handler runs in stateless
// mode: a request carrying an Mcp-Session-Id that this instance never issued
// must still succeed instead of returning 404 "session not found". This is what
// makes running more than one replica behind a load balancer safe — in stateful
// mode a follow-up request routed to a different pod than the one that handled
// initialize fails, surfacing to clients as "tools fetch failed". A regression
// back to stateful mode (opts nil / Stateless:false) fails this test.
func TestStatelessStreamableHandler(t *testing.T) {
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	ts := httptest.NewServer(newStatelessStreamableHandler(func(*http.Request) *mcp.Server {
		return srv
	}))
	defer ts.Close()

	t.Run("tools/list with unknown session returns 200, not 404", func(t *testing.T) {
		body := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`
		req, _ := http.NewRequest(http.MethodPost, ts.URL, bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		// A session id this instance never created — the exact multi-replica case.
		req.Header.Set("Mcp-Session-Id", "session-this-instance-never-issued")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()
		respBody, _ := io.ReadAll(resp.Body)

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("got HTTP %d, want 200 (stateful mode would 404 here); body: %s",
				resp.StatusCode, respBody)
		}
		if strings.Contains(string(respBody), "session not found") {
			t.Fatalf("response contains 'session not found' — handler is stateful, not stateless; body: %s", respBody)
		}
	})

	t.Run("GET SSE stream returns 405 in stateless mode", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, ts.URL, nil)
		req.Header.Set("Accept", "text/event-stream")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		// Stateless mode has no per-session push channel, so GET is 405.
		// Stateful mode would open a 200 SSE stream instead.
		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Fatalf("got HTTP %d, want 405", resp.StatusCode)
		}
	})
}
