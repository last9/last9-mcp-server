package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	last9mcp "github.com/last9/mcp-go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func newTestServer(t *testing.T) *last9mcp.Last9MCPServer {
	t.Helper()
	server, err := last9mcp.NewServerWithOptions("last9-mcp-test", "dev", last9mcp.WithSkipProviderInit())
	if err != nil {
		t.Fatal(err)
	}
	return server
}

func serveInMemory(t *testing.T, ctx context.Context, server *last9mcp.Last9MCPServer) (*mcp.ClientSession, chan error) {
	t.Helper()
	st, ct := mcp.NewInMemoryTransports()
	done := make(chan error, 1)
	go func() { done <- serveStdio(ctx, server, st) }()
	client := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "1"}, nil)
	cs, err := client.Connect(context.Background(), ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	return cs, done
}

func waitServe(t *testing.T, done chan error) {
	t.Helper()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serveStdio returned %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serveStdio did not return")
	}
}

func TestServeStdioClientCloseIsClean(t *testing.T) {
	cs, done := serveInMemory(t, context.Background(), newTestServer(t))
	cs.Close()
	waitServe(t, done)
}

func TestServeStdioCancelIsClean(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cs, done := serveInMemory(t, ctx, newTestServer(t))
	defer cs.Close()
	cancel()
	waitServe(t, done)
}

func TestServeStdioCancelAbortsInFlightToolCall(t *testing.T) {
	entered, cancelled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		close(entered)
		select {
		case <-r.Context().Done():
			close(cancelled)
		case <-release:
		}
	}))
	defer api.Close()

	cfg := testToolRegistrationConfig()
	cfg.APIBaseURL = api.URL
	server := newTestServer(t)
	if _, err := registerTools(server, cfg, api.Client()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cs, done := serveInMemory(t, ctx, server)
	defer cs.Close()
	defer close(release)

	go func() {
		_, _ = cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "get_databases", Arguments: map[string]any{}})
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("tool call never reached the API")
	}
	cancel()

	waitServe(t, done)
	select {
	case <-cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("the in-flight API request was never cancelled")
	}
}
