package main

import (
	"context"
	"testing"
	"time"

	last9mcp "github.com/last9/mcp-go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func serveInMemory(t *testing.T, ctx context.Context) (*mcp.ClientSession, chan error) {
	t.Helper()
	server, err := last9mcp.NewServerWithOptions("last9-mcp-test", "dev", last9mcp.WithSkipProviderInit())
	if err != nil {
		t.Fatal(err)
	}
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
	cs, done := serveInMemory(t, context.Background())
	cs.Close()
	waitServe(t, done)
}

func TestServeStdioCancelIsClean(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cs, done := serveInMemory(t, ctx)
	defer cs.Close()
	cancel()
	waitServe(t, done)
}
