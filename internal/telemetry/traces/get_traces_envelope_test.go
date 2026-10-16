package traces

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var nonEmptyServiceFilter = []map[string]interface{}{
	{"type": "filter", "query": map[string]interface{}{"$neq": []interface{}{"ServiceName", ""}}},
}

var exactTraceIDFilter = []map[string]interface{}{
	{"type": "filter", "query": map[string]interface{}{
		"$and": []interface{}{map[string]interface{}{"$eq": []interface{}{"TraceId", "ea8148dece205073096e4ad48145b08a"}}},
	}},
}

var avgDurationAggregate = []map[string]interface{}{
	{"type": "aggregate", "aggregates": []interface{}{
		map[string]interface{}{"function": map[string]interface{}{"$avg": []interface{}{"Duration"}}, "as": "avg"},
	}},
}

func callGetTraces(t *testing.T, handler http.HandlerFunc, args GetTracesArgs) *mcp.CallToolResult {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	cfg := testChunkTracesConfig(server.URL)
	cfg.MaxGetTracesEntries = 50
	result, _, err := NewGetTracesHandler(server.Client(), cfg)(context.Background(), &mcp.CallToolRequest{}, args)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if result.IsError {
		t.Fatalf("tool error: %s", result.Content[0].(*mcp.TextContent).Text)
	}
	return result
}

func TestGetTracesSingleRequestPathsProbeOnePastLimit(t *testing.T) {
	for name, pipeline := range map[string][]map[string]interface{}{
		"exact trace id": exactTraceIDFilter,
		"aggregate":      avgDurationAggregate,
	} {
		for _, tc := range []struct {
			returned      int
			wantTruncated bool
		}{{returned: 3}, {returned: 4, wantTruncated: true}} {
			var limits []string
			result := callGetTraces(t, func(w http.ResponseWriter, r *http.Request) {
				limits = append(limits, r.URL.Query().Get("limit"))
				_, _ = w.Write([]byte(traceAPIResponse(tc.returned)))
			}, GetTracesArgs{TracejsonQuery: pipeline, LookbackMinutes: 30, Limit: 3})

			if len(limits) != 1 || limits[0] != "4" {
				t.Fatalf("%s: requested limits = %v, want one request for limit+1", name, limits)
			}
			if got := countTracesInPayload(t, parseTracesToolResult(t, result)); got != 3 {
				t.Fatalf("%s returned %d: kept %d rows, want 3", name, tc.returned, got)
			}
			env := utils.GetResultEnvelope(t, result)
			if env.Truncated != tc.wantTruncated || (tc.wantTruncated && env.Limit != 3) {
				t.Fatalf("%s returned %d: envelope %+v, want truncated=%v", name, tc.returned, env, tc.wantTruncated)
			}
		}
	}
}

func TestGetTracesChunkedMarksTruncatedWhenChunksTogetherExceedLimit(t *testing.T) {
	rec := newTracesRequestRecorder()
	result := callGetTraces(t, func(w http.ResponseWriter, r *http.Request) {
		rec.add(r.URL.Query())
		_, _ = w.Write([]byte(traceAPIResponse(1)))
	}, GetTracesArgs{
		TracejsonQuery: nonEmptyServiceFilter,
		StartTimeISO:   "1970-01-01T00:00:00Z",
		EndTimeISO:     "1970-01-01T01:30:00Z",
		Limit:          5,
	})

	for _, q := range rec.all() {
		if q.Get("limit") != "6" {
			t.Fatalf("chunk limit = %q, want limit+1", q.Get("limit"))
		}
	}
	if got := countTracesInPayload(t, parseTracesToolResult(t, result)); got != 5 {
		t.Fatalf("traces = %d, want 5", got)
	}
	if env := utils.GetResultEnvelope(t, result); !env.Truncated || env.Limit != 5 {
		t.Fatalf("envelope = %+v, want truncated at 5", env)
	}
}

func TestGetTracesCompleteResultHasNoEnvelopeBlock(t *testing.T) {
	result := callGetTraces(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(traceAPIResponse(2)))
	}, GetTracesArgs{TracejsonQuery: nonEmptyServiceFilter, LookbackMinutes: 30, Limit: 3})

	if len(result.Content) != 1 {
		t.Fatalf("content blocks = %d, want only the payload", len(result.Content))
	}
	if env := utils.GetResultEnvelope(t, result); !env.IsEmpty() {
		t.Fatalf("envelope = %+v, want empty", env)
	}
}

func TestGetTracesEmptyFilterResultHintsDiscovery(t *testing.T) {
	result := callGetTraces(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(traceAPIResponse(0)))
	}, GetTracesArgs{TracejsonQuery: nonEmptyServiceFilter, LookbackMinutes: 30})

	env := utils.GetResultEnvelope(t, result)
	if len(env.Hints) != 1 || !strings.Contains(env.Hints[0], "get_trace_attributes_for_pipeline") {
		t.Fatalf("hints = %v, want one pointing at discovery", env.Hints)
	}
}

func TestGetTracesEmptyAggregateAddsNoHint(t *testing.T) {
	result := callGetTraces(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(traceAPIResponse(0)))
	}, GetTracesArgs{TracejsonQuery: avgDurationAggregate, LookbackMinutes: 30})

	if env := utils.GetResultEnvelope(t, result); !env.IsEmpty() {
		t.Fatalf("envelope = %+v, want empty for an aggregate", env)
	}
}

func TestGetTracesWarnsWhenLimitIsCapped(t *testing.T) {
	result := callGetTraces(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(traceAPIResponse(1)))
	}, GetTracesArgs{TracejsonQuery: nonEmptyServiceFilter, LookbackMinutes: 30, Limit: 500})

	env := utils.GetResultEnvelope(t, result)
	if len(env.Hints) != 1 || !strings.Contains(env.Hints[0], "500") || !strings.Contains(env.Hints[0], "50") {
		t.Fatalf("hints = %v, want one naming the requested and capped limits", env.Hints)
	}
}

func TestGetTracesTrimsOversizedResponse(t *testing.T) {
	result := callGetTraces(t, func(w http.ResponseWriter, r *http.Request) {
		items := make([]any, 40)
		for i := range items {
			items[i] = map[string]any{"TraceId": "t", "SpanName": strings.Repeat("x", 4000)}
		}
		body, _ := json.Marshal(map[string]any{"data": map[string]any{"result": items}})
		_, _ = w.Write(body)
	}, GetTracesArgs{TracejsonQuery: nonEmptyServiceFilter, LookbackMinutes: 30})

	text := result.Content[0].(*mcp.TextContent).Text
	if len(text) > utils.MaxResultTextBytes {
		t.Fatalf("payload text is %d bytes, over budget", len(text))
	}
	kept := countTracesInPayload(t, parseTracesToolResult(t, result))
	if env := utils.GetResultEnvelope(t, result); kept == 0 || env.TrimmedItems != 40-kept {
		t.Fatalf("kept %d, trimmed_items %d, want the two to sum to 40", kept, env.TrimmedItems)
	}
}
