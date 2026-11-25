package main

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"last9-mcp/internal/models"
	"last9-mcp/internal/toolsets"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// cancelProbe is a tool that runs until its context ends and reports when it
// started and when it saw the end, so a test can check what a client cancel reaches.
type cancelProbe struct {
	started chan struct{}
	ended   chan error
}

func registerCancelProbe(t *testing.T) (*cancelProbe, func() *mcp.ClientSession, func() string) {
	t.Helper()
	probe := &cancelProbe{started: make(chan struct{}, 1), ended: make(chan error, 1)}
	server := newRegisterTestServer(t)
	gate := newToolGate(toolsets.Filter{})
	gate.callTimeout = time.Minute
	tool := &mcp.Tool{Name: "cancel_probe", Annotations: readOnlyTool("Cancel Probe")}
	handler := func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		probe.started <- struct{}{}
		<-ctx.Done()
		probe.ended <- ctx.Err()
		return nil, nil, ctx.Err()
	}
	if err := registerIfAllowed(server, gate, tool, handler); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := gate.registerAdmitted(); err != nil {
		t.Fatalf("register admitted: %v", err)
	}
	inMemory := func() *mcp.ClientSession { return connectRegisteredServer(t, server) }
	httpURL := func() string {
		srv := httptest.NewServer(NewHTTPServer(server, models.Config{}, nil).Handler())
		t.Cleanup(srv.Close)
		return srv.URL
	}
	return probe, inMemory, httpURL
}

// cancelCallAndAwaitHandler starts the probe, cancels the client's call once the
// handler runs, and returns what the handler's context reported.
func cancelCallAndAwaitHandler(t *testing.T, session *mcp.ClientSession, probe *cancelProbe) error {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		<-probe.started
		cancel()
	}()
	if _, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "cancel_probe", Arguments: map[string]any{}}); !errors.Is(err, context.Canceled) {
		t.Errorf("client CallTool err = %v, want context.Canceled", err)
	}
	select {
	case err := <-probe.ended:
		return err
	case <-time.After(cancelReachBound):
		t.Fatalf("handler context still live %s after the client cancelled", cancelReachBound)
		return nil
	}
}

const cancelReachBound = 2 * time.Second

// stdio and the in-memory transport share the SDK's JSON-RPC connection, so a
// client's notifications/cancelled reaches the handler the same way on both.
func TestClientCancelReachesToolHandler_JSONRPCConnection(t *testing.T) {
	probe, inMemory, _ := registerCancelProbe(t)
	if err := cancelCallAndAwaitHandler(t, inMemory(), probe); !errors.Is(err, context.Canceled) {
		t.Fatalf("handler context ended with %v, want context.Canceled", err)
	}
}

// The SDK ties a call to its POST only from protocol 2026-07-28; older clients
// rely on the server ending the call when its POST ends.
func TestClientCancelReachesToolHandler_StatelessStreamableHTTP(t *testing.T) {
	for _, version := range []string{"", "2025-06-18"} {
		t.Run("protocol="+version, func(t *testing.T) {
			probe, _, httpURL := registerCancelProbe(t)
			session, err := mcp.NewClient(&mcp.Implementation{Name: "cancel-test", Version: Version}, nil).
				Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: httpURL()}, &mcp.ClientSessionOptions{ProtocolVersion: version})
			if err != nil {
				t.Fatalf("connect client: %v", err)
			}
			t.Cleanup(func() { _ = session.Close() })
			if err := cancelCallAndAwaitHandler(t, session, probe); !errors.Is(err, context.Canceled) {
				t.Fatalf("handler context ended with %v, want context.Canceled", err)
			}
		})
	}
}
