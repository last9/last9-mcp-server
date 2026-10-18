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
	"sync/atomic"
	"testing"

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

// newToolsetHTTPServer serves --http with startup toolsets spec through
// main's per-request builder and counts how many MCP servers it builds.
func newToolsetHTTPServer(t *testing.T, spec string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	allowed, err := toolsets.Parse(spec)
	if err != nil {
		t.Fatal(err)
	}
	cfg := testToolRegistrationConfig()
	cfg.AllowedTools = allowed
	builds := &atomic.Int32{}
	buildFor := toolsetServerBuilder(cfg)
	build := func(allowed toolsets.Set) (*last9mcp.Last9MCPServer, error) {
		builds.Add(1)
		return buildFor(allowed)
	}
	startup, err := build(allowed)
	if err != nil {
		t.Fatal(err)
	}
	h := NewHTTPServer(startup, cfg, build)
	ts := httptest.NewServer(h.Handler())
	t.Cleanup(ts.Close)
	return ts, builds
}

// postRPC posts one JSON-RPC body with an optional toolsets header and query,
// and decodes the result into out when the server answers 200.
func postRPC(t *testing.T, ts *httptest.Server, header, query, body string, out any) (int, string) {
	t.Helper()
	url, headers := rpcTarget(ts, header, query)
	return postRPCWithHeaders(t, url, headers, body, out)
}

func rpcTarget(ts *httptest.Server, header, query string) (string, []string) {
	url := ts.URL + "/mcp"
	if query != "" {
		url += "?toolsets=" + query
	}
	if header == "" {
		return url, nil
	}
	return url, []string{header}
}

// postRPCWithHeaders sends each of headers as a separate X-Last9-Toolsets line.
func postRPCWithHeaders(t *testing.T, url string, headers []string, body string, out any) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	for _, header := range headers {
		req.Header.Add("X-Last9-Toolsets", header)
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
	url, headers := rpcTarget(ts, header, query)
	return listToolsWithHeaders(t, url, headers)
}

func listToolsWithHeaders(t *testing.T, url string, headers []string) (int, string, map[string]bool) {
	t.Helper()
	var result struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	status, body := postRPCWithHeaders(t, url, headers, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, &result)
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
	ts, _ := newToolsetHTTPServer(t, "logs,alerts")
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

// Narrowing gates tools/call too, not only what tools/list advertises.
func TestHTTPToolsetsRejectCallsOutsideTheEffectiveSet(t *testing.T) {
	ts, _ := newToolsetHTTPServer(t, "logs,alerts")
	const call = `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_alerts","arguments":{}}}`
	status, body := postRPC(t, ts, "logs", "", call, &struct{}{})
	if status != http.StatusOK || !strings.Contains(body, `"error"`) || !strings.Contains(body, "get_alerts") {
		t.Fatalf("status %d body %q, want a JSON-RPC error naming get_alerts", status, body)
	}
}

func TestHTTPToolsetsJoinsRepeatedHeaders(t *testing.T) {
	ts, _ := newToolsetHTTPServer(t, "logs,alerts,traces")
	status, body, got := listToolsWithHeaders(t, ts.URL+"/mcp", []string{"logs", "alerts"})
	if status != http.StatusOK {
		t.Fatalf("status %d: %s", status, body)
	}
	assertTools(t, got, []string{"get_logs", "get_alerts"}, []string{"get_traces"})
}

// A header line with no toolsets in it does not override the query parameter.
func TestHTTPToolsetsBlankHeaderDefersToQuery(t *testing.T) {
	ts, _ := newToolsetHTTPServer(t, "logs,alerts")
	for _, headers := range [][]string{{""}, {" "}, {"", " "}} {
		status, body, got := listToolsWithHeaders(t, ts.URL+"/mcp?toolsets=alerts", headers)
		if status != http.StatusOK {
			t.Fatalf("headers %q: status %d: %s", headers, status, body)
		}
		assertTools(t, got, []string{"get_alerts"}, []string{"get_logs"})
	}
}

func TestHTTPToolsetsNarrowInitializeInstructions(t *testing.T) {
	ts, _ := newToolsetHTTPServer(t, "logs,alerts")
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
	ts, _ := newToolsetHTTPServer(t, "")
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

// drainTracker records whether the handler read the request body to EOF.
type drainTracker struct {
	io.Reader
	drained bool
}

func (d *drainTracker) Read(p []byte) (int, error) {
	n, err := d.Reader.Read(p)
	if err == io.EOF {
		d.drained = true
	}
	return n, err
}

// Requests rejected before reaching an MCP server still drain their body, so
// keep-alive clients can reuse the connection; a failed build is a generic 500.
func TestHTTPToolsetsRejectionsDrainTheBody(t *testing.T) {
	startup, err := last9mcp.NewServerWithOptions("t", "0", last9mcp.WithSkipProviderInit())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = startup.Shutdown(context.Background()) })
	builds := 0
	h := NewHTTPServer(startup, testToolRegistrationConfig(), func(toolsets.Set) (*last9mcp.Last9MCPServer, error) {
		builds++
		return nil, errors.New("boom: internal detail")
	})
	tests := []struct {
		header   string
		status   int
		wantBody string
	}{
		{header: "nope", status: http.StatusBadRequest, wantBody: `unknown toolset "nope"`},
		{header: "logs", status: http.StatusInternalServerError, wantBody: "failed to build server for the requested toolsets"},
	}
	for _, tt := range tests {
		t.Run(tt.header, func(t *testing.T) {
			body := &drainTracker{Reader: strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)}
			req := httptest.NewRequest(http.MethodPost, "/mcp", body)
			req.Header.Set("X-Last9-Toolsets", tt.header)
			rec := httptest.NewRecorder()
			h.Handler().ServeHTTP(rec, req)
			if rec.Code != tt.status || !strings.Contains(rec.Body.String(), tt.wantBody) {
				t.Fatalf("status %d body %q, want %d containing %q", rec.Code, rec.Body, tt.status, tt.wantBody)
			}
			if strings.Contains(rec.Body.String(), "internal detail") {
				t.Errorf("response leaks the build error: %q", rec.Body)
			}
			if !body.drained {
				t.Error("request body was not drained")
			}
		})
	}
	if builds != 1 {
		t.Fatalf("built %d servers, want 1", builds)
	}
}

func TestHTTPToolsetsFullStartupServesAnyRequestedSet(t *testing.T) {
	ts, _ := newToolsetHTTPServer(t, "")
	_, _, got := listTools(t, ts, "traces", "")
	assertTools(t, got, []string{"get_traces", "get_current_time"}, []string{"get_logs", "list_dashboards"})
	_, _, got = listTools(t, ts, "", "")
	assertTools(t, got, []string{"get_traces", "get_logs", "list_dashboards"}, nil)
}

func TestHTTPToolsetsCachesOneServerPerEffectiveSet(t *testing.T) {
	ts, builds := newToolsetHTTPServer(t, "")
	for _, header := range []string{"logs", "LOGS", "logs,logs", "traces", "logs"} {
		listTools(t, ts, header, "")
	}
	// startup + logs + traces
	if n := builds.Load(); n != 3 {
		t.Fatalf("built %d servers, want 3", n)
	}
}

func TestHTTPToolsetsRestatingStartupReusesIt(t *testing.T) {
	ts, builds := newToolsetHTTPServer(t, "logs,alerts")
	for _, header := range []string{"alerts,logs", "logs,alerts,alerts", "all"} {
		_, _, got := listTools(t, ts, header, "")
		assertTools(t, got, []string{"get_logs", "get_alerts"}, []string{"get_traces"})
	}
	if n := builds.Load(); n != 1 {
		t.Fatalf("built %d servers, want only the startup one", n)
	}
}
