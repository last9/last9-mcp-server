package logs

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var rawErrorFilter = []map[string]interface{}{
	{"type": "filter", "query": map[string]interface{}{"$contains": []interface{}{"Body", "error"}}},
}

func streamsServer(t *testing.T, values []logValue, limits *[]string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		if limits != nil {
			*limits = append(*limits, r.URL.Query().Get("limit"))
		}
		// A success status lets get_service_logs verify coverage; get_logs ignores it.
		_, _ = w.Write([]byte(strings.Replace(streamsAPIResponse(values), "{", `{"status":"success",`, 1)))
	}))
	t.Cleanup(server.Close)
	return server
}

func callGetLogs(t *testing.T, server *httptest.Server, args GetLogsArgs) *mcp.CallToolResult {
	t.Helper()
	cfg := testLogsConfig(server.URL)
	cfg.MaxGetLogsEntries = 50
	result, _, err := NewGetLogsHandler(server.Client(), cfg)(context.Background(), &mcp.CallToolRequest{}, args)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	return result
}

func nLogValues(n int, message string) []logValue {
	values := make([]logValue, n)
	for i := range values {
		values[i] = logValue{Timestamp: "60000000000", Message: message}
	}
	return values
}

func TestGetLogsRawProbesOnePastLimitAndMarksTruncated(t *testing.T) {
	for _, tc := range []struct {
		name          string
		matched       int
		wantTruncated bool
	}{
		{name: "fewer than limit", matched: 2},
		{name: "exactly limit", matched: 3},
		{name: "one past limit", matched: 4, wantTruncated: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var limits []string
			server := streamsServer(t, nLogValues(tc.matched, "boom"), &limits)
			result := callGetLogs(t, server, GetLogsArgs{LogjsonQuery: rawErrorFilter, LookbackMinutes: 5, Limit: 3})

			if len(limits) != 1 || limits[0] != "4" {
				t.Fatalf("requested limits = %v, want one probe for limit+1", limits)
			}
			payload := parseToolJSONResult(t, result)
			if got, want := countEntriesInPayload(t, payload), min(tc.matched, 3); got != want {
				t.Fatalf("entries = %d, want %d", got, want)
			}
			env := utils.GetResultEnvelope(t, result)
			if env.Truncated != tc.wantTruncated {
				t.Fatalf("truncated = %v, want %v", env.Truncated, tc.wantTruncated)
			}
			if tc.wantTruncated && env.Limit != 3 {
				t.Fatalf("limit = %d, want 3", env.Limit)
			}
			if !tc.wantTruncated && len(result.Content) != 1 {
				t.Fatalf("complete result must not carry an envelope block, got %d blocks", len(result.Content))
			}
		})
	}
}

func TestGetLogsChunkedRawMarksTruncatedWhenChunksTogetherExceedLimit(t *testing.T) {
	var limits []string
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		mu.Lock()
		limits = append(limits, r.URL.Query().Get("limit"))
		mu.Unlock()
		_, _ = w.Write([]byte(streamsAPIResponse(nLogValues(1, "boom"))))
	}))
	t.Cleanup(server.Close)

	result := callGetLogs(t, server, GetLogsArgs{
		LogjsonQuery: rawErrorFilter,
		StartTimeISO: "1970-01-01T00:00:00Z",
		EndTimeISO:   "1970-01-01T01:30:00Z",
		Limit:        5,
	})

	if len(limits) != 6 {
		t.Fatalf("chunk requests = %d, want 6", len(limits))
	}
	if got := countEntriesInPayload(t, parseToolJSONResult(t, result)); got != 5 {
		t.Fatalf("entries = %d, want 5", got)
	}
	if env := utils.GetResultEnvelope(t, result); !env.Truncated || env.Limit != 5 {
		t.Fatalf("envelope = %+v, want truncated at 5 when six chunks each return one line", env)
	}
}

func TestGetLogsRawEmptyResultHintsDiscovery(t *testing.T) {
	server := streamsServer(t, nil, nil)
	result := callGetLogs(t, server, GetLogsArgs{LogjsonQuery: rawErrorFilter, LookbackMinutes: 5})

	env := utils.GetResultEnvelope(t, result)
	if len(env.Hints) != 1 || !strings.Contains(env.Hints[0], "get_log_attributes_for_pipeline") {
		t.Fatalf("hints = %v, want one pointing at discovery", env.Hints)
	}
	block := result.Content[1].(*mcp.TextContent).Text
	if !strings.HasPrefix(block, "envelope: ") || !strings.Contains(block, "get_log_attributes_for_pipeline") {
		t.Fatalf("envelope block = %q", block)
	}
}

func TestGetLogsAggregateEmptyResultAddsNoHint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"matrix","result":[]}}`))
	}))
	t.Cleanup(server.Close)
	result := callGetLogs(t, server, GetLogsArgs{
		LogjsonQuery: []map[string]interface{}{testCountAggregate()}, LookbackMinutes: 5,
	})
	if env := utils.GetResultEnvelope(t, result); !env.IsEmpty() {
		t.Fatalf("envelope = %+v, want empty: aggregate answers stand on their own", env)
	}
}

func TestGetLogsWarnsWhenLimitIsCapped(t *testing.T) {
	server := streamsServer(t, nLogValues(1, "boom"), nil)
	result := callGetLogs(t, server, GetLogsArgs{LogjsonQuery: rawErrorFilter, LookbackMinutes: 5, Limit: 500})

	env := utils.GetResultEnvelope(t, result)
	if len(env.Hints) != 1 || !strings.Contains(env.Hints[0], "500") || !strings.Contains(env.Hints[0], "50") {
		t.Fatalf("hints = %v, want one naming the requested and capped limits", env.Hints)
	}
}

func TestGetLogsTrimsOversizedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		streams := make([]any, 40)
		for i := range streams {
			streams[i] = map[string]any{
				"stream": map[string]any{"severity": "error"},
				"values": [][]string{{"60000000000", strings.Repeat("x", 4000)}},
			}
		}
		body, _ := json.Marshal(map[string]any{"data": map[string]any{"resultType": "streams", "result": streams}})
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)

	result := callGetLogs(t, server, GetLogsArgs{LogjsonQuery: rawErrorFilter, LookbackMinutes: 5})

	text := result.Content[0].(*mcp.TextContent).Text
	if len(text) > utils.MaxResultTextBytes {
		t.Fatalf("payload text is %d bytes, over the %d budget", len(text), utils.MaxResultTextBytes)
	}
	kept := countEntriesInPayload(t, parseToolJSONResult(t, result))
	env := utils.GetResultEnvelope(t, result)
	if kept == 0 || env.TrimmedItems != 40-kept {
		t.Fatalf("kept %d, trimmed_items %d, want the two to sum to 40", kept, env.TrimmedItems)
	}
}

func TestGetLogsAggregateRowCapUsesEnvelope(t *testing.T) {
	srv, _ := aggregateSearchServer(t, 12)
	defer srv.Close()

	result, _, err := NewGetLogsHandler(srv.Client(), apiModeCfg(srv.URL))(context.Background(), &mcp.CallToolRequest{}, GetLogsArgs{
		LogjsonQuery: countAggregatePipeline(), LookbackMinutes: 5, Limit: 10,
	})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if _, present := parseToolJSONResult(t, result)["l9_result"]; present {
		t.Fatal("l9_result is superseded by the envelope")
	}
	if env := utils.GetResultEnvelope(t, result); !env.Truncated || env.Limit != 10 {
		t.Fatalf("envelope = %+v, want truncated at 10", env)
	}
}

func TestGetLogsSearchAPIRawProbesOnePastLimit(t *testing.T) {
	for _, tc := range []struct {
		name          string
		returned      int
		logsTruncated string
		wantTruncated bool
	}{
		{name: "complete", returned: 2, logsTruncated: "false"},
		{name: "probe row came back", returned: 3, logsTruncated: "false", wantTruncated: true},
		{name: "server says truncated", returned: 2, logsTruncated: "true", wantTruncated: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var sentLimit any
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				raw, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(raw, &body)
				sentLimit = body["limit"]
				var inner map[string]any
				_ = json.Unmarshal([]byte(streamsAPIResponse(nLogValues(tc.returned, "boom"))), &inner)
				out, _ := json.Marshal(map[string]any{"query_result": inner, "logs_truncated": tc.logsTruncated == "true"})
				_, _ = w.Write(out)
			}))
			defer srv.Close()

			result, _, err := NewGetLogsHandler(srv.Client(), apiModeCfg(srv.URL))(context.Background(), &mcp.CallToolRequest{}, GetLogsArgs{
				LogjsonQuery: rawErrorFilter, LookbackMinutes: 5, Limit: 2,
			})
			if err != nil {
				t.Fatalf("handler returned error: %v", err)
			}
			if sentLimit != float64(3) {
				t.Fatalf("sent limit = %v, want limit+1", sentLimit)
			}
			if got := countEntriesInPayload(t, parseToolJSONResult(t, result)); got != 2 {
				t.Fatalf("entries = %d, want 2", got)
			}
			if env := utils.GetResultEnvelope(t, result); env.Truncated != tc.wantTruncated {
				t.Fatalf("truncated = %v, want %v", env.Truncated, tc.wantTruncated)
			}
		})
	}
}
