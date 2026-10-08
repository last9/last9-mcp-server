package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/last9api"
	"last9-mcp/internal/models"

	last9mcp "github.com/last9/mcp-go-sdk/mcp"
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
	srv, err := last9mcp.NewServerWithOptions("test", "0", last9mcp.WithSkipProviderInit())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Shutdown(context.Background()) })

	ts := httptest.NewServer(srv.NewStreamableHTTPHandler(&mcp.StreamableHTTPOptions{Stateless: true}))
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

func TestStatelessHTTPUserToken(t *testing.T) {
	var mu sync.Mutex
	seen := map[string]string{}
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen[r.URL.Query().Get("id")] = r.Header.Get("X-LAST9-API-TOKEN")
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer api.Close()

	cfg := models.Config{HTTPMode: true, APIBaseURL: api.URL, UserTokenFallback: false, TokenManager: &auth.TokenManager{}}
	srv, err := last9mcp.NewServerWithOptions("test", "0", last9mcp.WithSkipProviderInit())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Shutdown(context.Background()) })
	srv.Server.AddReceivingMiddleware(userTokenMiddleware(cfg))
	mcp.AddTool(srv.Server, &mcp.Tool{Name: "probe"}, func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
		ID string `json:"id"`
	}) (*mcp.CallToolResult, any, error) {
		err := last9api.NewClient(api.Client(), cfg).Do(ctx, "probe", last9api.Request{Method: http.MethodGet, Path: "/probe?id=" + in.ID}, nil)
		return &mcp.CallToolResult{}, nil, err
	})
	ts := httptest.NewServer(srv.NewStreamableHTTPHandler(&mcp.StreamableHTTPOptions{Stateless: true}))
	defer ts.Close()

	post := func(body string, bearer ...string) map[string]any {
		t.Helper()
		req, err := http.NewRequest(http.MethodPost, ts.URL, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		if len(bearer) > 0 {
			req.Header.Set("Authorization", "Bearer "+bearer[0])
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		data, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(string(data), "event:") {
			for _, line := range strings.Split(string(data), "\n") {
				if strings.HasPrefix(line, "data: ") {
					data = []byte(strings.TrimPrefix(line, "data: "))
					break
				}
			}
		}
		var result map[string]any
		if err := json.Unmarshal(data, &result); err != nil {
			t.Fatalf("response %q: %v", data, err)
		}
		return result
	}
	for _, body := range []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"0"}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
	} {
		if result := post(body); result["error"] != nil {
			t.Fatalf("tokenless request failed: %v", result["error"])
		}
	}
	if result := post(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"probe","arguments":{"id":"missing"}}}`); result["error"] == nil {
		t.Fatalf("tokenless call succeeded: %v", result)
	}

	tokens := []string{
		testUserJWT(t, map[string]any{"exp": time.Now().Add(time.Hour).Unix(), "aud": []string{api.URL}}),
		testUserJWT(t, map[string]any{"exp": time.Now().Add(2 * time.Hour).Unix(), "aud": []string{api.URL}}),
	}
	var wg sync.WaitGroup
	for i, token := range tokens {
		wg.Add(1)
		go func(i int, token string) {
			defer wg.Done()
			body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": i + 4, "method": "tools/call", "params": map[string]any{"name": "probe", "arguments": map[string]any{"id": string(rune('a' + i))}, "_meta": map[string]any{"last9_user_token": token}}})
			if result := post(string(body)); result["error"] != nil {
				t.Errorf("call %d: %v", i, result["error"])
			}
		}(i, token)
	}
	wg.Wait()
	if result := post(`{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"probe","arguments":{"id":"header"}}}`, tokens[0]); result["error"] != nil {
		t.Fatalf("header call: %v", result["error"])
	}
	mu.Lock()
	defer mu.Unlock()
	if seen["a"] != "Bearer "+tokens[0] || seen["b"] != "Bearer "+tokens[1] || seen["header"] != "Bearer "+tokens[0] {
		t.Fatalf("downstream tokens did not match their calls: %v", seen)
	}
}

// TestHandleHealthReportsBuildVersion pins that /health reports the ldflag
// Version var, not a hardcoded literal (this regressed once before).
func TestHandleHealthReportsBuildVersion(t *testing.T) {
	// Mutates the package-global Version: this test must not run under
	// t.Parallel() alongside anything that reads Version.
	const sentinel = "9.9.9-test-version"
	orig := Version
	Version = sentinel
	t.Cleanup(func() { Version = orig })

	// Wire /health exactly as HTTPServer.Start does.
	h := &HTTPServer{}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", h.handleHealth)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatalf("GET /health failed: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("got status %d, want 200; body: %s", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("got Content-Type %q, want application/json", ct)
	}

	var got map[string]string
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode health body %q: %v", body, err)
	}

	if got["status"] != "healthy" {
		t.Errorf("status = %q, want healthy", got["status"])
	}
	if got["server"] != "last9-mcp" {
		t.Errorf("server = %q, want last9-mcp", got["server"])
	}
	if got["version"] != sentinel {
		t.Errorf("version = %q, want %q (the package-level Version var)", got["version"], sentinel)
	}
}
