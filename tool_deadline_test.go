package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"last9-mcp/internal/apm"
	"last9-mcp/internal/auth"
	"last9-mcp/internal/last9api"
	"last9-mcp/internal/models"
	"last9-mcp/internal/telemetry/logs"
	"last9-mcp/internal/telemetry/traces"
	"last9-mcp/internal/toolsets"

	last9mcp "github.com/last9/mcp-go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// A stalled upstream answers no chunk, so without an overall deadline a wide
// get_logs window runs one PerChunkHTTPTimeout per round of parallel chunks.
func TestToolCallEndsAtDeadlineWhenUpstreamStalls(t *testing.T) {
	var requests atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		stall(r)
	}))
	t.Cleanup(upstream.Close)

	cfg := models.Config{
		APIBaseURL:        upstream.URL,
		Region:            "us-east-1",
		MaxGetLogsEntries: models.DefaultMaxGetLogsEntries,
		TokenManager:      &auth.TokenManager{AccessToken: "test-token", ExpiresAt: time.Now().Add(time.Hour)},
	}
	server := newRegisterTestServer(t)
	gate := newToolGate(toolsets.Filter{})
	const deadline = 300 * time.Millisecond
	gate.callTimeout = deadline
	tool := &mcp.Tool{Name: "get_logs", Annotations: readOnlyTool("Get Logs")}
	if err := registerIfAllowed(server, gate, tool, logs.NewGetLogsHandler(upstream.Client(), cfg)); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := gate.registerAdmitted(); err != nil {
		t.Fatalf("register admitted: %v", err)
	}
	session := connectRegisteredServer(t, server)

	end := time.Now().UTC()
	args, _ := json.Marshal(map[string]any{
		"logjson_query":  []map[string]any{{"type": "filter", "query": map[string]any{"$contains": []any{"Body", "timeout"}}}},
		"start_time_iso": end.Add(-24 * time.Hour).Format(time.RFC3339),
		"end_time_iso":   end.Format(time.RFC3339),
	})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	started := time.Now()
	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "get_logs", Arguments: json.RawMessage(args)})
	elapsed := time.Since(started)

	if err != nil {
		t.Fatalf("call did not return within the client's patience: %v (after %s)", err, elapsed)
	}
	if limit := deadline + 2*time.Second; elapsed > limit {
		t.Fatalf("call stayed open %s, want at most %s", elapsed, limit)
	}
	if !res.IsError {
		t.Fatalf("want an error result once every chunk missed the deadline, got %q", toolResultText(res))
	}
	if text := toolResultText(res); !strings.Contains(text, "tool-call deadline") || !strings.Contains(text, "Narrow the time range") {
		t.Fatalf("want the deadline named with next steps, got %q", text)
	}
	if requests.Load() == 0 {
		t.Fatal("upstream was never called; the test did not exercise a stalled request")
	}
}

func TestWriteToolAtDeadlineNamesVerifyCall(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { stall(r) }))
	t.Cleanup(upstream.Close)

	cfg := writeToolTestConfig(upstream.URL)
	server := newRegisterTestServer(t)
	gate := newToolGate(toolsets.Filter{})
	gate.callTimeout = 300 * time.Millisecond
	create := func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		err := last9api.NewClient(upstream.Client(), cfg).Do(ctx, "create foo", last9api.Request{Method: http.MethodPost, Path: "/foos"}, nil)
		return nil, nil, err
	}
	tool := &mcp.Tool{Name: "create_foo", Annotations: writeTool("Create Foo", false, false)}
	if err := registerWriteTool(server, gate, tool, "get_foos, by name", create); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := gate.registerAdmitted(); err != nil {
		t.Fatalf("register admitted: %v", err)
	}
	session := connectRegisteredServer(t, server)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "create_foo", Arguments: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	text := toolResultText(res)
	if !res.IsError || !strings.Contains(text, "may or may not have been applied") || !strings.Contains(text, "get_foos, by name") {
		t.Fatalf("want the unknown-outcome result naming the verify call, got %q", text)
	}
}

// Handlers that turn a canceled request into a result (a partial_errors entry,
// an "invalid response" text) return no Go error, yet the call still ran out
// of time and must say so.
func TestReadToolResultAtDeadlineIsDeadlineError(t *testing.T) {
	tests := []struct {
		name     string
		tool     string
		upstream http.HandlerFunc
		args     map[string]any
	}{
		{
			name: "trace body stalls after headers",
			tool: "get_traces",
			upstream: func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = io.WriteString(w, `{"data":`)
				w.(http.Flusher).Flush()
				<-r.Context().Done()
			},
			args: map[string]any{
				"tracejson_query":  []map[string]any{{"type": "filter", "query": map[string]any{"$eq": []any{"ServiceName", "checkout"}}}},
				"lookback_minutes": 60,
			},
		},
		{
			name: "APM chunks return partial errors",
			tool: "get_service_performance_details",
			upstream: func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				select {
				case <-r.Context().Done():
				case <-time.After(100 * time.Millisecond):
					_, _ = io.WriteString(w, `[]`)
				}
			},
			args: map[string]any{"service_name": "checkout", "lookback_minutes": 60 * 24 * 60},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			upstream := httptest.NewServer(tt.upstream)
			t.Cleanup(upstream.Close)
			cfg := writeToolTestConfig(upstream.URL)

			server := newRegisterTestServer(t)
			gate := newToolGate(toolsets.Filter{})
			gate.callTimeout = 300 * time.Millisecond
			tool := &mcp.Tool{Name: tt.tool, Annotations: readOnlyTool(tt.tool)}
			var err error
			switch tt.tool {
			case "get_traces":
				err = registerIfAllowed(server, gate, tool, traces.NewGetTracesHandler(upstream.Client(), cfg))
			default:
				err = registerIfAllowed(server, gate, tool, apm.NewServicePerformanceDetailsHandler(upstream.Client(), cfg))
			}
			if err != nil {
				t.Fatalf("register: %v", err)
			}
			if err := gate.registerAdmitted(); err != nil {
				t.Fatalf("register admitted: %v", err)
			}
			session := connectRegisteredServer(t, server)

			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: tt.tool, Arguments: tt.args})
			if err != nil {
				t.Fatalf("call: %v", err)
			}
			if text := toolResultText(res); !res.IsError || !strings.Contains(text, "tool-call deadline") || !strings.Contains(text, "Narrow the time range") {
				t.Fatalf("want an isError result naming the deadline, got isError=%v %q", res.IsError, text)
			}
		})
	}
}

// stall answers nothing until the caller gives up. The body is drained first
// because the server notices a closed connection only after reading it.
func stall(r *http.Request) {
	_, _ = io.Copy(io.Discard, r.Body)
	<-r.Context().Done()
}

func connectRegisteredServer(t *testing.T, server *last9mcp.Last9MCPServer) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("connect server: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })
	session, err := mcp.NewClient(&mcp.Implementation{Name: "deadline-test", Version: Version}, nil).Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("connect client: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}
