package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime/debug"
	"strings"
	"testing"
	"time"

	last9mcp "github.com/last9/mcp-go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestENG1726_PinnedSDKIsV012(t *testing.T) {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		t.Fatal("ReadBuildInfo failed")
	}
	var ver string
	for _, dep := range info.Deps {
		if dep.Path == "github.com/last9/mcp-go-sdk" {
			ver = dep.Version
			break
		}
	}
	if ver != "v0.1.2" {
		t.Fatalf("hosted MCP is pinned to mcp-go-sdk %s; ENG-1726 evidence is for v0.1.2 (unknown_client on stateless HTTP)", ver)
	}
}

type hostedNoopArgs struct{}

func setupHostedTraceVerify(t *testing.T) (*httptest.Server, *tracetest.InMemoryExporter) {
	t.Helper()
	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp), sdktrace.WithSampler(sdktrace.AlwaysSample()))
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(sdkmetric.NewManualReader()))
	prevTP, prevMP := otel.GetTracerProvider(), otel.GetMeterProvider()
	otel.SetTracerProvider(tp)
	otel.SetMeterProvider(mp)

	server, err := last9mcp.NewServerWithOptions("last9-mcp", "test", last9mcp.WithSkipProviderInit())
	if err != nil {
		t.Fatalf("NewServerWithOptions: %v", err)
	}
	if err := last9mcp.RegisterInstrumentedTool(server, &mcp.Tool{Name: "noop", Description: "noop"},
		func(_ context.Context, _ *mcp.CallToolRequest, _ hostedNoopArgs) (*mcp.CallToolResult, any, error) {
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "ok"}}}, nil, nil
		},
	); err != nil {
		t.Fatalf("RegisterInstrumentedTool: %v", err)
	}

	ts := httptest.NewServer(newStatelessStreamableHandler(func(*http.Request) *mcp.Server {
		return server.Server
	}))
	t.Cleanup(func() {
		ts.Close()
		_ = server.Shutdown(context.Background())
		_ = tp.Shutdown(context.Background())
		otel.SetTracerProvider(prevTP)
		otel.SetMeterProvider(prevMP)
	})
	return ts, exp
}

func postMCP(t *testing.T, ts *httptest.Server, body string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, ts.URL, bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("got HTTP %d: %s", resp.StatusCode, raw)
	}
}

func toolCallBody(id int, name string) string {
	return fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"tools/call","params":{"name":%q,"arguments":{}}}`, id, name)
}

func attrString(span tracetest.SpanStub, key string) string {
	for _, a := range span.Attributes {
		if string(a.Key) == key {
			return a.Value.AsString()
		}
	}
	return ""
}

// TestENG1726_HostedStatelessCallsCollapseToUnknownClient reproduces hosted
// Streamable HTTP: tools/call never receives initialize identity, so every
// tool span is attributed to unknown_client and shares latestQuery(unknown_client).
func TestENG1726_HostedStatelessCallsCollapseToUnknownClient(t *testing.T) {
	ts, exp := setupHostedTraceVerify(t)
	postMCP(t, ts, toolCallBody(1, "noop"))
	postMCP(t, ts, toolCallBody(2, "noop"))

	spans := exp.GetSpans()
	var toolSpans []tracetest.SpanStub
	for _, sp := range spans {
		if strings.HasPrefix(sp.Name, "mcp tools/call ") {
			toolSpans = append(toolSpans, sp)
		}
	}
	if len(toolSpans) < 2 {
		dump, _ := json.Marshal(spanNames(spans))
		t.Fatalf("expected at least 2 tool spans, got %d names=%s", len(toolSpans), dump)
	}
	for _, sp := range toolSpans {
		if got := attrString(sp, "mcp.client.id"); got != "unknown_client" {
			t.Fatalf("span %q mcp.client.id=%q, want unknown_client on current v0.1.2 hosted path", sp.Name, got)
		}
	}
}

// TestENG1727_UserQueryParentEndsBeforeToolChildren records that the first
// tools/call ends mcp user_query immediately, then starts the tool span as a
// child of that already-ended interval.
func TestENG1727_UserQueryParentEndsBeforeToolChildren(t *testing.T) {
	ts, exp := setupHostedTraceVerify(t)
	postMCP(t, ts, toolCallBody(1, "noop"))
	time.Sleep(5 * time.Millisecond)
	postMCP(t, ts, toolCallBody(2, "noop"))

	spans := exp.GetSpans()
	var query, tool *tracetest.SpanStub
	for i := range spans {
		switch {
		case spans[i].Name == "mcp user_query" && query == nil:
			query = &spans[i]
		case strings.HasPrefix(spans[i].Name, "mcp tools/call ") && tool == nil:
			tool = &spans[i]
		}
	}
	if query == nil || tool == nil {
		t.Fatalf("missing spans: query=%v tool=%v names=%v", query != nil, tool != nil, spanNames(spans))
	}
	if !query.SpanContext.IsValid() {
		t.Fatal("user_query span context invalid")
	}
	if tool.Parent.SpanID() != query.SpanContext.SpanID() && tool.SpanContext.TraceID() != query.SpanContext.TraceID() {
		t.Fatalf("tool span is not in the user_query trace: parent=%s query=%s", tool.Parent.SpanID(), query.SpanContext.SpanID())
	}
	if !query.EndTime.Before(tool.StartTime) && !query.EndTime.Equal(tool.StartTime) {
		t.Fatalf("expected ended user_query (end=%s) at or before tool start (%s); topology may already be valid", query.EndTime, tool.StartTime)
	}
	if query.EndTime.After(tool.StartTime) {
		t.Fatal("user_query still open during tool span; ENG-1727 defect not present")
	}
}

func spanNames(spans tracetest.SpanStubs) []string {
	out := make([]string, len(spans))
	for i, sp := range spans {
		out[i] = sp.Name
	}
	return out
}
