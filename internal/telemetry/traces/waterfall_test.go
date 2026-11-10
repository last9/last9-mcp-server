package traces

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	apitraces "last9-mcp/internal/last9api/traces"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type waterfallRoundTripFunc func(*http.Request) (*http.Response, error)

func waterfallFixture(t *testing.T) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Clean(waterfallFixturePath))
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func waterfallFixtureWith(t *testing.T, edit func(map[string]any)) []byte {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(waterfallFixture(t), &payload); err != nil {
		t.Fatal(err)
	}
	edit(payload)
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func paddedWaterfall(t *testing.T, size int) []byte {
	t.Helper()
	base := waterfallFixtureWith(t, func(p map[string]any) { p["padding"] = "" })
	pad := size - len(base)
	if pad < 0 {
		t.Fatalf("fixture is already %d bytes", len(base))
	}
	return waterfallFixtureWith(t, func(p map[string]any) { p["padding"] = strings.Repeat("a", pad) })
}

type waterfallCall struct {
	result  *mcp.CallToolResult
	err     error
	request *http.Request
}

func callWaterfallHandler(t *testing.T, status int, body []byte, args GetTraceWaterfallArgs) waterfallCall {
	t.Helper()
	var call waterfallCall
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call.request = r
		w.WriteHeader(status)
		_, _ = w.Write(body)
	}))
	defer server.Close()
	handler := NewGetTraceWaterfallHandler(server.Client(), verifyTraceCfg(server.URL))
	call.result, _, call.err = handler(context.Background(), &mcp.CallToolRequest{}, args)
	return call
}

func waterfallText(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	if result == nil || len(result.Content) != 1 {
		t.Fatalf("want one content item, got %+v", result)
	}
	text, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("unexpected content type %T", result.Content[0])
	}
	return text.Text
}

func TestTraceWaterfallHandlerPreservesOrganizationRedaction(t *testing.T) {
	const redacted = "[REDACTED]"
	var payload map[string]any
	if err := json.Unmarshal(waterfallFixture(t), &payload); err != nil {
		t.Fatal(err)
	}
	payload["data"].(map[string]any)["selected_span"] = map[string]any{
		"span_id": "0123456789abcdef", "span_attributes": map[string]any{"account.reference": redacted},
	}
	payload["sanitizer"] = map[string]any{"version": traceEvidenceSanitizerVersion}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/waterfall/") {
			_, _ = w.Write(body)
			return
		}
		_, _ = io.WriteString(w, `{"traces":[{"TraceId":"0123456789abcdef0123456789abcdef","SpanId":"0123456789abcdef","Timestamp":"2026-07-15T08:30:00Z","Duration":1000,"attributes":{"account.reference":"synthetic-account-42"}}]}`)
	}))
	defer server.Close()
	handler := NewGetTraceWaterfallHandler(server.Client(), verifyTraceCfg(server.URL))
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetTraceWaterfallArgs{
		TraceID: "0123456789abcdef0123456789abcdef", SelectedSpanID: "0123456789abcdef",
		StartTimeISO: "2026-07-15T08:30:00Z", EndTimeISO: "2026-07-15T08:45:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	got := waterfallText(t, result)
	if !strings.Contains(got, `"account.reference":"`+redacted+`"`) || strings.Contains(got, "synthetic-account-42") {
		t.Fatalf("organization redaction was not preserved: %s", got)
	}
	structured, ok := result.StructuredContent.(json.RawMessage)
	if !ok || !bytes.Equal(structured, body) || got != string(body) {
		t.Fatalf("service bytes changed between MCP outputs: text=%q structured=%q", got, structured)
	}
}

func (f waterfallRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestTraceWaterfallHandlerRejectsOversizedUpstreamBodyBeforeDecode(t *testing.T) {
	validBody := paddedWaterfall(t, apitraces.MaxWaterfallBytes+1)
	client := &http.Client{Transport: waterfallRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewReader(validBody)),
			Header:     make(http.Header),
		}, nil
	})}
	handler := NewGetTraceWaterfallHandler(client, tracesTestConfig("https://example.test"))
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetTraceWaterfallArgs{TraceID: "0123456789abcdef0123456789abcdef"})
	if err == nil || !strings.Contains(err.Error(), "exceeds the 524288-byte output cap") {
		t.Fatalf("expected bounded-body error, got %v", err)
	}
}

func TestTraceWaterfallHandlerAcceptsBodyAtUpstreamLimit(t *testing.T) {
	validBody := paddedWaterfall(t, apitraces.MaxWaterfallBytes)
	client := &http.Client{Transport: waterfallRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(validBody)), Header: make(http.Header)}, nil
	})}
	handler := NewGetTraceWaterfallHandler(client, tracesTestConfig("https://example.test"))
	if _, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetTraceWaterfallArgs{TraceID: "0123456789abcdef0123456789abcdef"}); err != nil {
		t.Fatalf("body at limit: %v", err)
	}
}

func TestTraceWaterfallHandlerPreservesEmptySpanArrays(t *testing.T) {
	body := waterfallFixtureWith(t, func(payload map[string]any) {
		data := payload["data"].(map[string]any)
		data["spans"] = []any{}
		data["slowest_spans"] = []any{}
		data["largest_self_time_contributors"] = []any{}
	})
	client := &http.Client{Transport: waterfallRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(body)), Header: make(http.Header)}, nil
	})}
	handler := NewGetTraceWaterfallHandler(client, tracesTestConfig("https://example.test"))
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetTraceWaterfallArgs{TraceID: "0123456789abcdef0123456789abcdef"})
	if err != nil {
		t.Fatal(err)
	}
	text := result.Content[0].(*mcp.TextContent)
	var payload struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal([]byte(text.Text), &payload); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"spans", "slowest_spans", "largest_self_time_contributors"} {
		if string(payload.Data[field]) != "[]" {
			t.Errorf("data.%s = %s, want []", field, payload.Data[field])
		}
	}
}
