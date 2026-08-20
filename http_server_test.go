package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	last9mcp "github.com/last9/mcp-go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func jsonRPCPayload(raw []byte) []byte {
	s := string(raw)
	if !strings.Contains(s, "data:") {
		return raw
	}
	var buf bytes.Buffer
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "data:") {
			buf.WriteString(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	if buf.Len() == 0 {
		return raw
	}
	return buf.Bytes()
}

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
	srv.Server.AddReceivingMiddleware(cacheTTLMiddleware)

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

	t.Run("server/discover returns supported versions without initialize", func(t *testing.T) {
		body := `{"jsonrpc":"2.0","id":1,"method":"server/discover","params":{"_meta":{"io.modelcontextprotocol/clientInfo":{"name":"test-client","version":"1.0.0"},"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}`
		req, _ := http.NewRequest(http.MethodPost, ts.URL, bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		req.Header.Set("Mcp-Protocol-Version", "2026-07-28")
		req.Header.Set("Mcp-Method", "server/discover")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()
		respBody, _ := io.ReadAll(resp.Body)

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("got HTTP %d, want 200; body: %s", resp.StatusCode, respBody)
		}
		var msg struct {
			Error  json.RawMessage `json:"error"`
			Result struct {
				SupportedVersions []string `json:"supportedVersions"`
				TTLMs             int      `json:"ttlMs"`
				CacheScope        string   `json:"cacheScope"`
			} `json:"result"`
		}
		if err := json.Unmarshal(jsonRPCPayload(respBody), &msg); err != nil {
			t.Fatalf("decode body: %v; body: %s", err, respBody)
		}
		if len(msg.Error) > 0 && string(msg.Error) != "null" {
			t.Fatalf("jsonrpc error: %s; body: %s", msg.Error, respBody)
		}
		hasCurrent, hasLegacy := false, false
		for _, v := range msg.Result.SupportedVersions {
			if v == "2026-07-28" {
				hasCurrent = true
			}
			if v == "2025-11-25" || v == "2025-06-18" || v == "2025-03-26" || v == "2024-11-05" {
				hasLegacy = true
			}
		}
		if !hasCurrent {
			t.Fatalf("supportedVersions missing 2026-07-28: %v", msg.Result.SupportedVersions)
		}
		if !hasLegacy {
			t.Fatalf("supportedVersions missing a legacy version: %v", msg.Result.SupportedVersions)
		}
		if msg.Result.TTLMs != serverDiscoverTTLMs {
			t.Fatalf("ttlMs = %d, want %d", msg.Result.TTLMs, serverDiscoverTTLMs)
		}
		if msg.Result.CacheScope != cacheScopePublic {
			t.Fatalf("cacheScope = %q, want %q", msg.Result.CacheScope, cacheScopePublic)
		}
	})

	t.Run("legacy initialize handshake still works", func(t *testing.T) {
		body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"legacy-client","version":"1.0.0"}}}`
		req, _ := http.NewRequest(http.MethodPost, ts.URL, bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		req.Header.Set("Mcp-Protocol-Version", "2025-11-25")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()
		respBody, _ := io.ReadAll(resp.Body)

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("got HTTP %d, want 200; body: %s", resp.StatusCode, respBody)
		}
		var msg struct {
			Error  json.RawMessage `json:"error"`
			Result struct {
				ProtocolVersion string `json:"protocolVersion"`
			} `json:"result"`
		}
		if err := json.Unmarshal(jsonRPCPayload(respBody), &msg); err != nil {
			t.Fatalf("decode body: %v; body: %s", err, respBody)
		}
		if len(msg.Error) > 0 && string(msg.Error) != "null" {
			t.Fatalf("jsonrpc error: %s; body: %s", msg.Error, respBody)
		}
		if msg.Result.ProtocolVersion != "2025-11-25" {
			t.Fatalf("protocolVersion = %q, want 2025-11-25", msg.Result.ProtocolVersion)
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
