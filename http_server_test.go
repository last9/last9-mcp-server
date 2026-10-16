package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"last9-mcp/internal/models"
	"last9-mcp/internal/toolsets"

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

// newToolsetHTTPServer serves --http with startup toolsets spec and counts how
// many MCP servers it builds.
func newToolsetHTTPServer(t *testing.T, spec string, maxCached int) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	allowed, err := toolsets.Parse(spec)
	if err != nil {
		t.Fatal(err)
	}
	cfg := testToolRegistrationConfig()
	cfg.AllowedTools = allowed
	builds := &atomic.Int32{}
	build := func(allowed toolsets.Set) (*last9mcp.Last9MCPServer, error) {
		builds.Add(1)
		c := cfg
		c.AllowedTools = allowed
		return buildServer(c)
	}
	startup, err := build(allowed)
	if err != nil {
		t.Fatal(err)
	}
	h := NewHTTPServer(startup, cfg, build)
	h.servers.maxCached = maxCached
	ts := httptest.NewServer(h.Handler())
	t.Cleanup(ts.Close)
	return ts, builds
}

// postRPC posts one JSON-RPC body with an optional toolsets header and query,
// and decodes the result into out when the server answers 200.
func postRPC(t *testing.T, ts *httptest.Server, header, query, body string, out any) (int, string) {
	t.Helper()
	url := ts.URL + "/mcp"
	if query != "" {
		url += "?toolsets=" + query
	}
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if header != "" {
		req.Header.Set("X-Last9-Toolsets", header)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, string(raw)
	}
	payload := string(raw)
	if i := strings.Index(payload, "data: "); i >= 0 {
		payload = strings.SplitN(payload[i+len("data: "):], "\n", 2)[0]
	}
	rpc := struct {
		Result any `json:"result"`
	}{Result: out}
	if err := json.Unmarshal([]byte(payload), &rpc); err != nil {
		t.Fatalf("decode %q: %v", raw, err)
	}
	return resp.StatusCode, string(raw)
}

// listTools posts tools/list with an optional toolsets header and query.
func listTools(t *testing.T, ts *httptest.Server, header, query string) (int, string, map[string]bool) {
	t.Helper()
	var result struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	status, body := postRPC(t, ts, header, query, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, &result)
	if status != http.StatusOK {
		return status, body, nil
	}
	names := map[string]bool{}
	for _, tool := range result.Tools {
		names[tool.Name] = true
	}
	return status, body, names
}

func assertTools(t *testing.T, got map[string]bool, want, deny []string) {
	t.Helper()
	for _, name := range want {
		if !got[name] {
			t.Errorf("tools/list missing %s", name)
		}
	}
	for _, name := range deny {
		if got[name] {
			t.Errorf("tools/list serves %s", name)
		}
	}
}

func TestHTTPToolsetsPerRequest(t *testing.T) {
	ts, _ := newToolsetHTTPServer(t, "logs,alerts", maxToolsetServers)
	core := []string{"get_current_time", "get_workspace_overview"}

	tests := []struct {
		name          string
		header, query string
		want, deny    []string
	}{
		{name: "no header serves the startup set", want: append([]string{"get_logs", "get_alerts"}, core...), deny: []string{"get_traces"}},
		{name: "header narrows", header: "logs", want: append([]string{"get_logs"}, core...), deny: []string{"get_alerts", "get_traces"}},
		{name: "header cannot widen", header: "traces", want: core, deny: []string{"get_traces", "get_logs", "get_alerts"}},
		{name: "header all keeps the startup set", header: "all", want: []string{"get_logs", "get_alerts"}, deny: []string{"get_traces"}},
		{name: "query param narrows", query: "alerts", want: append([]string{"get_alerts"}, core...), deny: []string{"get_logs"}},
		{name: "header beats query param", header: "logs", query: "alerts", want: []string{"get_logs"}, deny: []string{"get_alerts"}},
		{name: "whitespace and case", header: " Logs , ", want: []string{"get_logs"}, deny: []string{"get_alerts"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, body, got := listTools(t, ts, tt.header, tt.query)
			if status != http.StatusOK {
				t.Fatalf("status %d: %s", status, body)
			}
			assertTools(t, got, tt.want, tt.deny)
		})
	}
}

func TestHTTPToolsetsNarrowInitializeInstructions(t *testing.T) {
	ts, _ := newToolsetHTTPServer(t, "logs,alerts", maxToolsetServers)
	const initialize = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"0"}}}`
	tests := []struct {
		header     string
		want, deny []string
	}{
		{header: "", want: []string{"`get_logs`", "`get_alerts`"}},
		{header: "logs", want: []string{"`get_logs`"}, deny: []string{"`get_alerts`"}},
		{header: "alerts", want: []string{"`get_alerts`"}, deny: []string{"`get_logs`"}},
	}
	for _, tt := range tests {
		t.Run("header="+tt.header, func(t *testing.T) {
			var result struct {
				Instructions string `json:"instructions"`
			}
			if status, body := postRPC(t, ts, tt.header, "", initialize, &result); status != http.StatusOK {
				t.Fatalf("status %d: %s", status, body)
			}
			for _, name := range tt.want {
				if !strings.Contains(result.Instructions, name) {
					t.Errorf("instructions missing %s:\n%s", name, result.Instructions)
				}
			}
			for _, name := range tt.deny {
				if strings.Contains(result.Instructions, name) {
					t.Errorf("instructions name %s outside the requested toolsets:\n%s", name, result.Instructions)
				}
			}
		})
	}
}

func TestHTTPToolsetsUnknownNameIs400(t *testing.T) {
	ts, _ := newToolsetHTTPServer(t, "", maxToolsetServers)
	for _, tt := range []struct{ header, query string }{
		{header: "logs,nope"},
		{query: "nope"},
		{header: "nope", query: "logs"},
	} {
		status, body, _ := listTools(t, ts, tt.header, tt.query)
		if status != http.StatusBadRequest || !strings.Contains(body, `unknown toolset "nope"`) {
			t.Errorf("header %q query %q: status %d body %q, want 400 naming nope", tt.header, tt.query, status, body)
		}
	}
}

func TestHTTPToolsetsFullStartupServesAnyRequestedSet(t *testing.T) {
	ts, _ := newToolsetHTTPServer(t, "", maxToolsetServers)
	_, _, got := listTools(t, ts, "traces", "")
	assertTools(t, got, []string{"get_traces", "get_current_time"}, []string{"get_logs", "list_dashboards"})
	_, _, got = listTools(t, ts, "", "")
	assertTools(t, got, []string{"get_traces", "get_logs", "list_dashboards"}, nil)
}

func TestHTTPToolsetsCachesOneServerPerEffectiveSet(t *testing.T) {
	ts, builds := newToolsetHTTPServer(t, "", maxToolsetServers)
	for _, header := range []string{"logs", "LOGS", "logs,logs", "traces", "logs"} {
		listTools(t, ts, header, "")
	}
	// startup + logs + traces
	if n := builds.Load(); n != 3 {
		t.Fatalf("built %d servers, want 3", n)
	}
}

func TestHTTPToolsetsCacheIsBounded(t *testing.T) {
	ts, builds := newToolsetHTTPServer(t, "", 1)
	for _, header := range []string{"logs", "traces", "traces", "logs"} {
		_, _, got := listTools(t, ts, header, "")
		if !got["get_"+header] {
			t.Fatalf("header %s: tools/list = %v", header, got)
		}
	}
	// startup, logs (cached), then traces twice uncached.
	if n := builds.Load(); n != 4 {
		t.Fatalf("built %d servers, want 4", n)
	}
}

// blockingBuilds builds a bare server per set; a build for gate's tool waits
// until release is closed, after signalling started.
type blockingBuilds struct {
	gate     string
	started  chan struct{}
	release  chan struct{}
	opened   sync.Once
	failNext atomic.Bool
	builds   sync.Map // tool set key -> *atomic.Int32
}

func (b *blockingBuilds) build(allowed toolsets.Set) (*last9mcp.Last9MCPServer, error) {
	n, _ := b.builds.LoadOrStore(toolSetKey(allowed), &atomic.Int32{})
	n.(*atomic.Int32).Add(1)
	if _, ok := allowed[b.gate]; ok {
		b.started <- struct{}{}
		<-b.release
	}
	if b.failNext.Swap(false) {
		return nil, errors.New("build failed")
	}
	return last9mcp.NewServerWithOptions("test", "0", last9mcp.WithSkipProviderInit())
}

func (b *blockingBuilds) open() { b.opened.Do(func() { close(b.release) }) }

func (b *blockingBuilds) count(allowed toolsets.Set) int32 {
	n, ok := b.builds.Load(toolSetKey(allowed))
	if !ok {
		return 0
	}
	return n.(*atomic.Int32).Load()
}

func newBlockingCache(t *testing.T) (*toolsetServers, *blockingBuilds) {
	t.Helper()
	b := &blockingBuilds{gate: "get_logs", started: make(chan struct{}, 8), release: make(chan struct{})}
	c := NewHTTPServer(nil, models.Config{}, b.build).servers
	t.Cleanup(func() {
		b.open()
		_ = c.shutdown(context.Background())
	})
	return c, b
}

func handlerWithin(t *testing.T, c *toolsetServers, allowed toolsets.Set) http.Handler {
	t.Helper()
	type result struct {
		h   http.Handler
		err error
	}
	done := make(chan result, 1)
	go func() {
		h, release, err := c.handler(allowed)
		if release != nil {
			release()
		}
		done <- result{h, err}
	}()
	select {
	case r := <-done:
		if r.err != nil {
			t.Fatal(r.err)
		}
		return r.h
	case <-time.After(5 * time.Second):
		t.Fatalf("handler(%v) blocked behind another set's build", allowed)
		return nil
	}
}

func TestToolsetServersBuildOutsideTheLock(t *testing.T) {
	c, b := newBlockingCache(t)
	logs := toolsets.Set{"get_logs": {}}
	traces := toolsets.Set{"get_traces": {}}
	metrics := toolsets.Set{"prometheus_range_query": {}}
	handlerWithin(t, c, traces)

	slow := make(chan error, 1)
	go func() {
		_, _, err := c.handler(logs)
		slow <- err
	}()
	<-b.started

	handlerWithin(t, c, traces)
	handlerWithin(t, c, metrics)
	b.open()
	if err := <-slow; err != nil {
		t.Fatal(err)
	}
}

func TestToolsetServersBuildEachSetOnce(t *testing.T) {
	c, b := newBlockingCache(t)
	logs := toolsets.Set{"get_logs": {}}
	handlers := make(chan http.Handler, 2)
	for range 2 {
		go func() {
			h, _, err := c.handler(logs)
			if err != nil {
				t.Error(err)
			}
			handlers <- h
		}()
	}
	<-b.started
	time.Sleep(50 * time.Millisecond)
	b.open()
	first, second := <-handlers, <-handlers
	if first == nil || first != second {
		t.Fatalf("concurrent misses got handlers %p and %p, want one shared handler", first, second)
	}
	if n := b.count(logs); n != 1 {
		t.Fatalf("built logs %d times, want 1", n)
	}
}

func TestToolsetServersRetryAFailedBuild(t *testing.T) {
	c, b := newBlockingCache(t)
	traces := toolsets.Set{"get_traces": {}}
	b.failNext.Store(true)
	if _, _, err := c.handler(traces); err == nil {
		t.Fatal("want the build error")
	}
	handlerWithin(t, c, traces)
	if n := b.count(traces); n != 2 {
		t.Fatalf("built traces %d times, want a retry after the failure", n)
	}
}
