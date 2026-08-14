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

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Production evidence from ENG-1728: a 16-hex span ID was forwarded as trace_id.
const eng1728SpanIDAsTraceID = "31298f07314ea1b9"

func countingTraceDetailsServer(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var n atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/cat/api/traces/") {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":"Invalid traceId"}`)
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

// TestENG1728_WaterfallForwardsSpanIDAsTraceID records the current leak:
// a 16-hex value is accepted as trace_id and sent upstream. The fix must
// reject locally and keep this count at 0.
func TestENG1728_WaterfallForwardsSpanIDAsTraceID(t *testing.T) {
	server, n := countingTraceDetailsServer(t)
	handler := NewGetTraceWaterfallHandler(server.Client(), verifyTraceCfg(server.URL))
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetTraceWaterfallArgs{
		TraceID: eng1728SpanIDAsTraceID,
	})
	if err == nil {
		t.Fatal("expected upstream error for 16-hex trace_id, got nil")
	}
	if got := n.Load(); got != 1 {
		t.Fatalf("ENG-1728 leak changed: waterfall 16-hex trace_id made %d upstream requests, want 1 (current bug) / 0 (fixed)", got)
	}
}

// TestENG1728_ServiceTracesForwardsSpanIDAsTraceID is the same leak on
// get_service_traces exact lookup, which also hits the trace-details endpoint.
func TestENG1728_ServiceTracesForwardsSpanIDAsTraceID(t *testing.T) {
	server, n := countingTraceDetailsServer(t)
	handler := GetServiceTracesHandler(server.Client(), verifyTraceCfg(server.URL))
	_, _, _ = handler(context.Background(), &mcp.CallToolRequest{}, GetServiceTracesArgs{
		TraceID: eng1728SpanIDAsTraceID,
	})
	if got := n.Load(); got != 1 {
		t.Fatalf("ENG-1728 leak changed: get_service_traces 16-hex trace_id made %d upstream requests, want 1 (current bug) / 0 (fixed)", got)
	}
}

// TestENG1728_GetTracesExactLookupForwardsSpanID records that extractExactTraceIDLookup
// treats a 16-hex equality as an exact trace ID and still issues an upstream request.
func TestENG1728_GetTracesExactLookupForwardsSpanID(t *testing.T) {
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
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if got := n.Load(); got < 1 {
		t.Fatal("expected get_traces exact 16-hex lookup to hit upstream; got 0")
	}
}
