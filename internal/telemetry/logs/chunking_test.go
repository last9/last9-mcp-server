package logs

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// 30-minute chunk size means:
//   60-minute window → 2 chunks: newest [1800s,3600s] and oldest [0,1800s]
//   90-minute window → 3 chunks: [3600,5400], [1800,3600], [0,1800]

func TestGetLogsHandlerChunksRawQueriesAndHonorsLimit(t *testing.T) {
	rec := newRequestRecorder()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		rec.add(q)
		switch q.Get("start") + "-" + q.Get("end") {
		case "1800-3600":
			_, _ = w.Write([]byte(streamsAPIResponse(
				[]logValue{{Timestamp: "3600000000000", Message: "latest"}, {Timestamp: "2700000000000", Message: "recent"}},
			)))
		case "0-1800":
			_, _ = w.Write([]byte(streamsAPIResponse(
				[]logValue{{Timestamp: "60000000000", Message: "older"}},
			)))
		default:
			t.Errorf("unexpected chunk %s", q.Encode())
			http.Error(w, "unexpected chunk", http.StatusBadRequest)
		}
	}))
	defer server.Close()

	handler := NewGetLogsHandler(server.Client(), testLogsConfig(server.URL))
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetLogsArgs{
		LogjsonQuery: []map[string]interface{}{
			{
				"type":  "filter",
				"query": map[string]interface{}{"$contains": []interface{}{"Body", "error"}},
			},
		},
		StartTimeISO: "1970-01-01T00:00:00Z",
		EndTimeISO:   "1970-01-01T01:00:00Z",
		Limit:        3,
	})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if got := rec.count(); got != 2 {
		t.Fatalf("expected 2 chunk requests, got %d", got)
	}
	// Each chunk now receives effectiveLimit; post-merge truncation enforces the cap.
	for _, q := range rec.all() {
		if got := q.Get("limit"); got != "3" {
			t.Fatalf("expected every chunk limit=3 (effective), got %q", got)
		}
	}

	payload := parseToolJSONResult(t, result)
	if entryCount := countEntriesInPayload(t, payload); entryCount != 3 {
		t.Fatalf("expected 3 log entries in merged payload, got %d", entryCount)
	}
}

func TestGetLogsHandlerChunksRawQueriesWithoutLimit(t *testing.T) {
	rec := newRequestRecorder()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		rec.add(q)
		switch q.Get("start") + "-" + q.Get("end") {
		case "1800-3600":
			_, _ = w.Write([]byte(streamsAPIResponse(
				[]logValue{{Timestamp: "3600000000000", Message: "latest"}, {Timestamp: "2700000000000", Message: "recent"}},
			)))
		case "0-1800":
			_, _ = w.Write([]byte(streamsAPIResponse(
				[]logValue{{Timestamp: "60000000000", Message: "older"}},
			)))
		default:
			t.Errorf("unexpected chunk %s", q.Encode())
			http.Error(w, "unexpected chunk", http.StatusBadRequest)
		}
	}))
	defer server.Close()

	cfg := testLogsConfig(server.URL)
	cfg.MaxGetLogsEntries = 3
	handler := NewGetLogsHandler(server.Client(), cfg)
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetLogsArgs{
		LogjsonQuery: []map[string]interface{}{
			{
				"type":  "filter",
				"query": map[string]interface{}{"$contains": []interface{}{"Body", "error"}},
			},
		},
		StartTimeISO: "1970-01-01T00:00:00Z",
		EndTimeISO:   "1970-01-01T01:00:00Z",
	})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if got := rec.count(); got != 2 {
		t.Fatalf("expected 2 chunk requests, got %d", got)
	}

	payload := parseToolJSONResult(t, result)
	if entryCount := countEntriesInPayload(t, payload); entryCount != 3 {
		t.Fatalf("expected 3 merged log entries in payload, got %d", entryCount)
	}
}

func TestGetLogsHandlerCapsExplicitLimitAtConfiguredMax(t *testing.T) {
	rec := newRequestRecorder()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		rec.add(q)
		switch q.Get("start") + "-" + q.Get("end") {
		case "1800-3600":
			_, _ = w.Write([]byte(streamsAPIResponse(
				[]logValue{{Timestamp: "3600000000000", Message: "latest"}, {Timestamp: "2700000000000", Message: "recent"}},
			)))
		case "0-1800":
			_, _ = w.Write([]byte(streamsAPIResponse(
				[]logValue{{Timestamp: "60000000000", Message: "older"}},
			)))
		default:
			t.Errorf("unexpected chunk %s", q.Encode())
			http.Error(w, "unexpected chunk", http.StatusBadRequest)
		}
	}))
	defer server.Close()

	cfg := testLogsConfig(server.URL)
	cfg.MaxGetLogsEntries = 3
	handler := NewGetLogsHandler(server.Client(), cfg)
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetLogsArgs{
		LogjsonQuery: []map[string]interface{}{
			{
				"type":  "filter",
				"query": map[string]interface{}{"$contains": []interface{}{"Body", "error"}},
			},
		},
		StartTimeISO: "1970-01-01T00:00:00Z",
		EndTimeISO:   "1970-01-01T01:00:00Z",
		Limit:        10,
	})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if got := rec.count(); got != 2 {
		t.Fatalf("expected 2 chunk requests, got %d", got)
	}
	for _, q := range rec.all() {
		if got := q.Get("limit"); got != "3" {
			t.Fatalf("expected every chunk capped to limit=3, got %q", got)
		}
	}

	payload := parseToolJSONResult(t, result)
	if entryCount := countEntriesInPayload(t, payload); entryCount != 3 {
		t.Fatalf("expected 3 merged log entries in payload, got %d", entryCount)
	}
}

func TestGetLogsHandlerDoesNotChunkAggregateQueries(t *testing.T) {
	rec := newRequestRecorder()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.add(r.URL.Query())
		_, _ = w.Write([]byte(`{"data":{"resultType":"matrix","result":[]}}`))
	}))
	defer server.Close()

	handler := NewGetLogsHandler(server.Client(), testLogsConfig(server.URL))
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetLogsArgs{
		LogjsonQuery: []map[string]interface{}{
			{
				"type":  "filter",
				"query": map[string]interface{}{"$contains": []interface{}{"Body", "error"}},
			},
			{"type": "aggregate"},
		},
		StartTimeISO: "1970-01-01T00:00:00Z",
		EndTimeISO:   "1970-01-01T01:00:00Z",
		Limit:        3,
	})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if got := rec.count(); got != 1 {
		t.Fatalf("expected aggregate query to stay single-call, got %d requests", got)
	}
}

func TestGetLogsHandlerErrorsOnNonStreamChunkResult(t *testing.T) {
	rec := newRequestRecorder()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.add(r.URL.Query())
		_, _ = w.Write([]byte(`{"data":{"resultType":"matrix","result":[]}}`))
	}))
	defer server.Close()

	handler := NewGetLogsHandler(server.Client(), testLogsConfig(server.URL))
	// Single chunk → single non-stream response → caller returns error
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetLogsArgs{
		LogjsonQuery: []map[string]interface{}{
			{
				"type":  "filter",
				"query": map[string]interface{}{"$contains": []interface{}{"Body", "error"}},
			},
		},
		StartTimeISO: "1970-01-01T00:00:00Z",
		EndTimeISO:   "1970-01-01T00:10:00Z",
		Limit:        3,
	})
	if err == nil {
		t.Fatal("expected handler to fail for non-stream chunk result")
	}
	if err.Error() != `chunked get_logs expected streams result, got "matrix"` {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.count() != 1 {
		t.Fatalf("expected a single chunk request, got %d", rec.count())
	}
}

func TestGetLogsHandlerTreatsMissingStreamsResultAsEmptyChunk(t *testing.T) {
	rec := newRequestRecorder()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		rec.add(q)
		switch q.Get("start") + "-" + q.Get("end") {
		case "1800-3600":
			_, _ = w.Write([]byte(`{"data":{"resultType":"streams"}}`))
		case "0-1800":
			_, _ = w.Write([]byte(streamsAPIResponse(
				[]logValue{{Timestamp: "60000000000", Message: "older"}},
			)))
		default:
			t.Errorf("unexpected chunk %s", q.Encode())
			http.Error(w, "unexpected chunk", http.StatusBadRequest)
		}
	}))
	defer server.Close()

	handler := NewGetLogsHandler(server.Client(), testLogsConfig(server.URL))
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetLogsArgs{
		LogjsonQuery: []map[string]interface{}{
			{
				"type":  "filter",
				"query": map[string]interface{}{"$contains": []interface{}{"Body", "error"}},
			},
		},
		StartTimeISO: "1970-01-01T00:00:00Z",
		EndTimeISO:   "1970-01-01T01:00:00Z",
		Limit:        3,
	})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if rec.count() != 2 {
		t.Fatalf("expected 2 chunk requests, got %d", rec.count())
	}

	payload := parseToolJSONResult(t, result)
	if entryCount := countEntriesInPayload(t, payload); entryCount != 1 {
		t.Fatalf("expected 1 merged log entry in payload, got %d", entryCount)
	}
}

func TestGetLogsHandlerReturnsPartialResultsAfterLaterChunkParseError(t *testing.T) {
	rec := newRequestRecorder()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		rec.add(q)
		// 90-minute range → chunks [3600,5400], [1800,3600], [0,1800]; oldest fails parse.
		switch q.Get("start") + "-" + q.Get("end") {
		case "3600-5400":
			_, _ = w.Write([]byte(streamsAPIResponse(
				[]logValue{{Timestamp: "5400000000000", Message: "latest"}, {Timestamp: "4500000000000", Message: "recent"}},
			)))
		case "1800-3600":
			_, _ = w.Write([]byte(streamsAPIResponse(
				[]logValue{{Timestamp: "3000000000000", Message: "middle"}},
			)))
		case "0-1800":
			_, _ = w.Write([]byte(`{"data":{"resultType":"streams","result":{}}}`))
		default:
			t.Errorf("unexpected chunk %s", q.Encode())
			http.Error(w, "unexpected chunk", http.StatusBadRequest)
		}
	}))
	defer server.Close()

	handler := NewGetLogsHandler(server.Client(), testLogsConfig(server.URL))
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetLogsArgs{
		LogjsonQuery: []map[string]interface{}{
			{
				"type":  "filter",
				"query": map[string]interface{}{"$contains": []interface{}{"Body", "error"}},
			},
		},
		StartTimeISO: "1970-01-01T00:00:00Z",
		EndTimeISO:   "1970-01-01T01:30:00Z",
		Limit:        10,
	})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if rec.count() != 3 {
		t.Fatalf("expected 3 chunk requests, got %d", rec.count())
	}

	payload := parseToolJSONResult(t, result)
	if entryCount := countEntriesInPayload(t, payload); entryCount != 3 {
		t.Fatalf("expected 3 merged log entries in payload, got %d", entryCount)
	}

	meta, ok := payload[partialResultMetadataKey].(map[string]any)
	if !ok {
		t.Fatalf("expected partial metadata in payload, got %#v", payload)
	}
	if partial, ok := meta["partial_result"].(bool); !ok || !partial {
		t.Fatalf("expected partial_result=true, got %#v", meta["partial_result"])
	}
	warning, ok := meta["warning"].(string)
	if !ok || !strings.Contains(warning, "chunk 3/3 failed to parse") {
		t.Fatalf("expected parse warning in metadata, got %#v", meta["warning"])
	}
}

func TestFetchServiceLogsChunksAndHonorsEntryLimit(t *testing.T) {
	rec := newRequestRecorder()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		rec.add(q)
		switch q.Get("start") + "-" + q.Get("end") {
		case "1860-3660":
			_, _ = w.Write([]byte(streamsAPIResponse(
				[]logValue{{Timestamp: "3600000000000", Message: "latest"}, {Timestamp: "2700000000000", Message: "recent"}},
			)))
		case "60-1860":
			_, _ = w.Write([]byte(streamsAPIResponse(
				[]logValue{{Timestamp: "120000000000", Message: "older"}, {Timestamp: "90000000000", Message: "oldest"}},
			)))
		default:
			t.Errorf("unexpected chunk %s", q.Encode())
			http.Error(w, "unexpected chunk", http.StatusBadRequest)
		}
	}))
	defer server.Close()

	startTime := time.Unix(60, 0).UTC()
	endTime := startTime.Add(60 * time.Minute)

	response, err := fetchServiceLogs(
		context.Background(),
		server.Client(),
		testLogsConfig(server.URL),
		"api",
		startTime,
		endTime,
		3,
		buildServiceLogsQuery("api", []string{"error"}, []string{"timeout"}),
		"physical_index:payments",
	)
	if err != nil {
		t.Fatalf("fetchServiceLogs returned error: %v", err)
	}

	if rec.count() != 2 {
		t.Fatalf("expected 2 chunk requests, got %d", rec.count())
	}
	if response.Count != 3 {
		t.Fatalf("expected count=3, got %d", response.Count)
	}
	if len(response.Logs) != 3 {
		t.Fatalf("expected 3 logs, got %d", len(response.Logs))
	}
	// Newest chunk delivers 2 entries; second chunk's first entry is appended,
	// the rest are trimmed to honor the overall limit of 3.
	if response.Logs[2].Message != "older" {
		t.Fatalf("expected final retained log to be trimmed to 'older', got %#v", response.Logs[2])
	}
}

func TestFetchServiceLogsReturnsPartialResultsAfterLaterChunkError(t *testing.T) {
	rec := newRequestRecorder()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		rec.add(q)
		switch q.Get("start") + "-" + q.Get("end") {
		case "3600-5400":
			_, _ = w.Write([]byte(streamsAPIResponse(
				[]logValue{{Timestamp: "5400000000000", Message: "latest"}, {Timestamp: "4500000000000", Message: "recent"}},
			)))
		case "1800-3600":
			_, _ = w.Write([]byte(streamsAPIResponse(
				[]logValue{{Timestamp: "3000000000000", Message: "middle"}},
			)))
		case "0-1800":
			http.Error(w, "backend blew up", http.StatusInternalServerError)
		default:
			t.Errorf("unexpected chunk %s", q.Encode())
			http.Error(w, "unexpected chunk", http.StatusBadRequest)
		}
	}))
	defer server.Close()

	startTime := time.Unix(0, 0).UTC()
	endTime := startTime.Add(90 * time.Minute)

	response, err := fetchServiceLogs(
		context.Background(),
		server.Client(),
		testLogsConfig(server.URL),
		"api",
		startTime,
		endTime,
		10,
		buildServiceLogsQuery("api", nil, nil),
		"",
	)
	if err != nil {
		t.Fatalf("fetchServiceLogs returned error: %v", err)
	}

	if rec.count() != 3 {
		t.Fatalf("expected 3 chunk requests, got %d", rec.count())
	}
	if response.Count != 3 {
		t.Fatalf("expected count=3, got %d", response.Count)
	}
	if !response.PartialResult {
		t.Fatalf("expected partial result flag, got %#v", response)
	}
	if !strings.Contains(response.Warning, "chunk 3/") || !strings.Contains(response.Warning, "failed") {
		t.Fatalf("expected partial warning naming chunk 3, got %q", response.Warning)
	}
}

func TestParallelChunksRespectSemaphore(t *testing.T) {
	// 20-chunk fan-out (10 hours / 30 min = 20) must never exceed MaxParallelChunks in flight.
	var (
		inflight    atomic.Int32
		maxObserved atomic.Int32
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cur := inflight.Add(1)
		for {
			prev := maxObserved.Load()
			if cur <= prev || maxObserved.CompareAndSwap(prev, cur) {
				break
			}
		}
		// Hold the slot briefly so concurrency is observable.
		time.Sleep(20 * time.Millisecond)
		inflight.Add(-1)
		_, _ = w.Write([]byte(streamsAPIResponse(nil)))
	}))
	defer server.Close()

	handler := NewGetLogsHandler(server.Client(), testLogsConfig(server.URL))
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetLogsArgs{
		LogjsonQuery: []map[string]interface{}{
			{
				"type":  "filter",
				"query": map[string]interface{}{"$contains": []interface{}{"Body", "x"}},
			},
		},
		StartTimeISO: "1970-01-01T00:00:00Z",
		EndTimeISO:   "1970-01-01T10:00:00Z",
		Limit:        100,
	})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if got := maxObserved.Load(); got > 5 {
		t.Fatalf("expected at most 5 concurrent chunks, observed %d", got)
	}
	if maxObserved.Load() < 2 {
		t.Fatalf("expected concurrency > 1 to validate parallel execution, observed %d", maxObserved.Load())
	}
}

func TestParseTimeRangeFromArgsDefaultsToFiveMinutes(t *testing.T) {
	now := time.Date(2026, time.March, 12, 12, 0, 0, 0, time.UTC)

	startTime, endTime, err := parseTimeRangeFromArgsAt(GetLogsArgs{}, now)
	if err != nil {
		t.Fatalf("parseTimeRangeFromArgs returned error: %v", err)
	}

	duration := time.UnixMilli(endTime).Sub(time.UnixMilli(startTime))
	if duration != 5*time.Minute {
		t.Fatalf("expected exact 5 minute default lookback, got %s", duration)
	}
	if got := time.UnixMilli(endTime).UTC(); !got.Equal(now) {
		t.Fatalf("expected end time %s, got %s", now, got)
	}
}

func TestTruncateResultItemsByEntryLimitClonesValuesSlice(t *testing.T) {
	values := []interface{}{
		[]interface{}{"420000000000", "latest"},
		[]interface{}{"300000000000", "recent"},
	}
	items := []interface{}{
		map[string]interface{}{
			"stream": map[string]interface{}{
				"severity": "error",
			},
			"values": values,
		},
	}

	truncated := truncateResultItemsByEntryLimit(items, 1)
	stream, ok := truncated[0].(map[string]interface{})
	if !ok {
		t.Fatalf("expected stream map, got %T", truncated[0])
	}
	truncatedValues, ok := stream["values"].([]interface{})
	if !ok {
		t.Fatalf("expected values slice, got %T", stream["values"])
	}

	values[0] = []interface{}{"0", "mutated"}
	firstValue, ok := truncatedValues[0].([]interface{})
	if !ok {
		t.Fatalf("expected log value array, got %T", truncatedValues[0])
	}
	if got := firstValue[1]; got != "latest" {
		t.Fatalf("expected cloned slice to preserve original entry, got %#v", firstValue)
	}
}

// --- helpers ---

type requestRecorder struct {
	mu   sync.Mutex
	reqs []url.Values
}

func newRequestRecorder() *requestRecorder {
	return &requestRecorder{}
}

func (r *requestRecorder) add(q url.Values) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reqs = append(r.reqs, q)
}

func (r *requestRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.reqs)
}

func (r *requestRecorder) all() []url.Values {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]url.Values, len(r.reqs))
	copy(out, r.reqs)
	return out
}

type logValue struct {
	Timestamp string
	Message   string
}

func streamsAPIResponse(values []logValue) string {
	streamValues := make([][]string, 0, len(values))
	for _, value := range values {
		streamValues = append(streamValues, []string{value.Timestamp, value.Message})
	}

	body, err := json.Marshal(map[string]any{
		"data": map[string]any{
			"resultType": "streams",
			"result": []any{
				map[string]any{
					"stream": map[string]any{
						"severity": "error",
					},
					"values": streamValues,
				},
			},
		},
	})
	if err != nil {
		panic(err)
	}
	return string(body)
}

func parseToolJSONResult(t *testing.T, result *mcp.CallToolResult) map[string]any {
	t.Helper()

	if len(result.Content) == 0 {
		t.Fatal("expected tool content")
	}
	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected text content, got %T", result.Content[0])
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(textContent.Text), &payload); err != nil {
		t.Fatalf("failed to parse result JSON: %v", err)
	}

	return payload
}

func countEntriesInPayload(t *testing.T, payload map[string]any) int {
	t.Helper()

	data, ok := payload["data"].(map[string]any)
	if !ok {
		t.Fatalf("missing data object: %#v", payload)
	}

	result, ok := data["result"].([]any)
	if !ok {
		t.Fatalf("missing result array: %#v", payload)
	}

	total := 0
	for _, item := range result {
		stream, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("unexpected result item type: %T", item)
		}
		values, ok := stream["values"].([]any)
		if !ok {
			t.Fatalf("unexpected values type: %T", stream["values"])
		}
		total += len(values)
	}

	return total
}
