package traces

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/models"
	"last9-mcp/internal/otelids"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	eng1728SpanIDAsTraceID = "31298f07314ea1b9"
	eng1728ValidTraceID    = "ea8148dece205073096e4ad48145b08a"
	eng1728ValidSpanID     = "0123456789abcdef"
)

func countingTraceDetailsServer(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var n atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/cat/api/traces/") {
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"traces":[]}`)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"status":"success","data":{"result":[]}}`)
	}))
	t.Cleanup(server.Close)
	return server, &n
}

func verifyTraceCfg(apiBaseURL string) models.Config {
	return models.Config{
		APIBaseURL: apiBaseURL,
		Region:     "ap-south-1",
		TokenManager: &auth.TokenManager{
			AccessToken: "test-token",
			ExpiresAt:   time.Now().Add(24 * time.Hour),
		},
	}
}

func TestGetTraceWaterfall_InvalidIDsMakeZeroUpstreamRequests(t *testing.T) {
	cases := []struct {
		name     string
		args     GetTraceWaterfallArgs
		category string
	}{
		{name: "empty", args: GetTraceWaterfallArgs{}, category: otelids.CategoryInvalidTraceID},
		{name: "short", args: GetTraceWaterfallArgs{TraceID: "abc123"}, category: otelids.CategoryInvalidTraceID},
		{name: "long", args: GetTraceWaterfallArgs{TraceID: eng1728ValidTraceID + "00"}, category: otelids.CategoryInvalidTraceID},
		{name: "non-hex", args: GetTraceWaterfallArgs{TraceID: "ea8148dece205073096e4ad48145b0zz"}, category: otelids.CategoryInvalidTraceID},
		{name: "all-zero", args: GetTraceWaterfallArgs{TraceID: strings.Repeat("0", 32)}, category: otelids.CategoryAllZeroID},
		{name: "16-hex span id", args: GetTraceWaterfallArgs{TraceID: eng1728SpanIDAsTraceID}, category: otelids.CategorySpanIDAsTraceID},
		{name: "invalid selected span", args: GetTraceWaterfallArgs{TraceID: eng1728ValidTraceID, SelectedSpanID: "not-a-span"}, category: otelids.CategoryInvalidSpanID},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			server, n := countingTraceDetailsServer(t)
			handler := NewGetTraceWaterfallHandler(server.Client(), verifyTraceCfg(server.URL))
			_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, tt.args)
			if err == nil {
				t.Fatal("expected local validation error")
			}
			if !strings.Contains(err.Error(), "category="+tt.category) {
				t.Fatalf("want category %s, got %v", tt.category, err)
			}
			if tt.category == otelids.CategorySpanIDAsTraceID && !strings.Contains(err.Error(), "received a span ID where a trace ID is required") {
				t.Fatalf("missing span-id message: %v", err)
			}
			if got := n.Load(); got != 0 {
				t.Fatalf("invalid ID made %d upstream requests, want 0", got)
			}
		})
	}
}

func TestGetTraceWaterfall_ValidTraceIDHitsUpstream(t *testing.T) {
	server, n := countingTraceDetailsServer(t)
	handler := NewGetTraceWaterfallHandler(server.Client(), verifyTraceCfg(server.URL))
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetTraceWaterfallArgs{
		TraceID:        strings.ToUpper(eng1728ValidTraceID),
		SelectedSpanID: strings.ToUpper(eng1728ValidSpanID),
	})
	if err != nil {
		t.Fatalf("valid IDs should proceed: %v", err)
	}
	if got := n.Load(); got != 1 {
		t.Fatalf("valid waterfall made %d upstream requests, want 1", got)
	}
}

func TestGetServiceTraces_InvalidTraceIDMakesZeroUpstreamRequests(t *testing.T) {
	server, n := countingTraceDetailsServer(t)
	handler := GetServiceTracesHandler(server.Client(), verifyTraceCfg(server.URL))
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetServiceTracesArgs{
		TraceID: eng1728SpanIDAsTraceID,
	})
	if err == nil {
		t.Fatal("expected local validation error")
	}
	if !strings.Contains(err.Error(), "received a span ID where a trace ID is required") {
		t.Fatalf("got %v", err)
	}
	if got := n.Load(); got != 0 {
		t.Fatalf("get_service_traces made %d upstream requests, want 0", got)
	}
}

func TestGetTraces_InvalidExactTraceIDMakesZeroUpstreamRequests(t *testing.T) {
	server, n := countingTraceDetailsServer(t)
	handler := NewGetTracesHandler(server.Client(), testChunkTracesConfig(server.URL))
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetTracesArgs{
		TracejsonQuery: []map[string]interface{}{
			{
				"type": "filter",
				"query": map[string]interface{}{
					"$and": []interface{}{
						map[string]interface{}{
							"$eq": []interface{}{"TraceId", eng1728SpanIDAsTraceID},
						},
					},
				},
			},
		},
		StartTimeISO: "1970-01-01T00:00:00Z",
		EndTimeISO:   "1970-01-01T00:15:00Z",
	})
	if err == nil {
		t.Fatal("expected local validation error")
	}
	if !strings.Contains(err.Error(), "received a span ID where a trace ID is required") {
		t.Fatalf("got %v", err)
	}
	if got := n.Load(); got != 0 {
		t.Fatalf("get_traces made %d upstream requests, want 0", got)
	}
}

func TestGetTraceWaterfallInputSchemaOmitsIDPatterns(t *testing.T) {
	// A schema pattern is validated by the SDK before the handler runs, and its
	// regex message replaces the actionable one NormalizeTraceID produces.
	schema := GetTraceWaterfallInputSchema()
	props := schema["properties"].(map[string]interface{})
	for _, field := range []string{"trace_id", "selected_span_id"} {
		if p, ok := props[field].(map[string]interface{})["pattern"]; ok {
			t.Fatalf("%s must not carry a schema pattern, got %v", field, p)
		}
	}
}

func TestSpanIDAsTraceIDKeepsActionableMessage(t *testing.T) {
	server, n := countingTraceDetailsServer(t)
	handler := NewGetTraceWaterfallHandler(server.Client(), verifyTraceCfg(server.URL))
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetTraceWaterfallArgs{
		TraceID: eng1728SpanIDAsTraceID,
	})
	if err == nil {
		t.Fatal("expected rejection")
	}
	if !strings.Contains(err.Error(), "span ID where a trace ID is required") {
		t.Fatalf("error must name the span-ID mistake, got: %v", err)
	}
	if got := n.Load(); got != 0 {
		t.Fatalf("expected zero upstream requests, got %d", got)
	}
}
