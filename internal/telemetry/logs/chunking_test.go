package logs

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestGetLogsHandlerChunksRawQueriesAndHonorsLimit(t *testing.T) {
	requests := make([]url.Values, 0)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Query())

		switch len(requests) {
		case 1:
			_, _ = w.Write([]byte(streamsAPIResponse(
				[]logValue{{Timestamp: "420000000000", Message: "latest"}, {Timestamp: "300000000000", Message: "recent"}},
			)))
		case 2:
			_, _ = w.Write([]byte(streamsAPIResponse(
				[]logValue{{Timestamp: "60000000000", Message: "older"}},
			)))
		default:
			t.Fatalf("unexpected extra request %d", len(requests))
		}
	}))
	defer server.Close()

	handler := NewGetLogsHandler(server.Client(), testLogsConfig(server.URL))
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetLogsArgs{
		LogjsonQuery: []map[string]interface{}{
			{
				"type": "filter",
				"query": map[string]interface{}{
					"$contains": []interface{}{"Body", "error"},
				},
			},
		},
		StartTimeISO: "1970-01-01T00:00:00Z",
		EndTimeISO:   "1970-01-01T00:07:00Z",
		Limit:        3,
	})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if len(requests) != 2 {
		t.Fatalf("expected 2 chunk requests, got %d", len(requests))
	}
	if got := requests[0].Get("limit"); got != "3" {
		t.Fatalf("expected first request limit=3, got %q", got)
	}
	if got := requests[1].Get("limit"); got != "1" {
		t.Fatalf("expected second request limit=1, got %q", got)
	}
	if got := requests[0].Get("start"); got != "120" || requests[0].Get("end") != "420" {
		t.Fatalf("unexpected first chunk bounds: %v", requests[0])
	}
	if got := requests[1].Get("start"); got != "0" || requests[1].Get("end") != "120" {
		t.Fatalf("unexpected second chunk bounds: %v", requests[1])
	}

	payload := parseToolJSONResult(t, result)
	if entryCount := countEntriesInPayload(t, payload); entryCount != 3 {
		t.Fatalf("expected 3 log entries in merged payload, got %d", entryCount)
	}
}

func TestGetLogsHandlerChunksRawQueriesWithoutLimit(t *testing.T) {
	requests := make([]url.Values, 0)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Query())

		switch len(requests) {
		case 1:
			_, _ = w.Write([]byte(streamsAPIResponse(
				[]logValue{{Timestamp: "420000000000", Message: "latest"}, {Timestamp: "300000000000", Message: "recent"}},
			)))
		case 2:
			_, _ = w.Write([]byte(streamsAPIResponse(
				[]logValue{{Timestamp: "60000000000", Message: "older"}},
			)))
		default:
			t.Fatalf("unexpected extra request %d", len(requests))
		}
	}))
	defer server.Close()

	cfg := testLogsConfig(server.URL)
	cfg.MaxGetLogsEntries = 3
	handler := NewGetLogsHandler(server.Client(), cfg)
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetLogsArgs{
		LogjsonQuery: []map[string]interface{}{
			{
				"type": "filter",
				"query": map[string]interface{}{
					"$contains": []interface{}{"Body", "error"},
				},
			},
		},
		StartTimeISO: "1970-01-01T00:00:00Z",
		EndTimeISO:   "1970-01-01T00:07:00Z",
	})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if len(requests) != 2 {
		t.Fatalf("expected 2 chunk requests, got %d", len(requests))
	}
	if got := requests[0].Get("limit"); got != "3" {
		t.Fatalf("expected first request limit=3, got %q", got)
	}
	if got := requests[1].Get("limit"); got != "1" {
		t.Fatalf("expected second request limit=1, got %q", got)
	}
	if got := requests[0].Get("start"); got != "120" || requests[0].Get("end") != "420" {
		t.Fatalf("unexpected first chunk bounds: %v", requests[0])
	}
	if got := requests[1].Get("start"); got != "0" || requests[1].Get("end") != "120" {
		t.Fatalf("unexpected second chunk bounds: %v", requests[1])
	}

	payload := parseToolJSONResult(t, result)
	if entryCount := countEntriesInPayload(t, payload); entryCount != 3 {
		t.Fatalf("expected 3 merged log entries in payload, got %d", entryCount)
	}
}

func TestGetLogsHandlerCapsExplicitLimitAtConfiguredMax(t *testing.T) {
	requests := make([]url.Values, 0)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Query())

		switch len(requests) {
		case 1:
			_, _ = w.Write([]byte(streamsAPIResponse(
				[]logValue{{Timestamp: "420000000000", Message: "latest"}, {Timestamp: "300000000000", Message: "recent"}},
			)))
		case 2:
			_, _ = w.Write([]byte(streamsAPIResponse(
				[]logValue{{Timestamp: "60000000000", Message: "older"}},
			)))
		default:
			t.Fatalf("unexpected extra request %d", len(requests))
		}
	}))
	defer server.Close()

	cfg := testLogsConfig(server.URL)
	cfg.MaxGetLogsEntries = 3
	handler := NewGetLogsHandler(server.Client(), cfg)
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetLogsArgs{
		LogjsonQuery: []map[string]interface{}{
			{
				"type": "filter",
				"query": map[string]interface{}{
					"$contains": []interface{}{"Body", "error"},
				},
			},
		},
		StartTimeISO: "1970-01-01T00:00:00Z",
		EndTimeISO:   "1970-01-01T00:07:00Z",
		Limit:        10,
	})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if len(requests) != 2 {
		t.Fatalf("expected 2 chunk requests, got %d", len(requests))
	}
	if got := requests[0].Get("limit"); got != "3" {
		t.Fatalf("expected first request limit=3, got %q", got)
	}
	if got := requests[1].Get("limit"); got != "1" {
		t.Fatalf("expected second request limit=1, got %q", got)
	}

	payload := parseToolJSONResult(t, result)
	if entryCount := countEntriesInPayload(t, payload); entryCount != 3 {
		t.Fatalf("expected 3 merged log entries in payload, got %d", entryCount)
	}
}

func TestGetLogsHandlerDoesNotChunkAggregateQueries(t *testing.T) {
	requests := make([]url.Values, 0)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Query())
		_, _ = w.Write([]byte(`{"data":{"resultType":"matrix","result":[]}}`))
	}))
	defer server.Close()

	handler := NewGetLogsHandler(server.Client(), testLogsConfig(server.URL))
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetLogsArgs{
		LogjsonQuery: []map[string]interface{}{
			{
				"type": "filter",
				"query": map[string]interface{}{
					"$contains": []interface{}{"Body", "error"},
				},
			},
			{
				"type": "aggregate",
			},
		},
		StartTimeISO: "1970-01-01T00:00:00Z",
		EndTimeISO:   "1970-01-01T00:07:00Z",
		Limit:        3,
	})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if len(requests) != 1 {
		t.Fatalf("expected aggregate query to stay single-call, got %d requests", len(requests))
	}
	if got := requests[0].Get("start"); got != "0" || requests[0].Get("end") != "420" {
		t.Fatalf("unexpected aggregate request bounds: %v", requests[0])
	}
}

func TestGetLogsHandlerErrorsOnNonStreamChunkResult(t *testing.T) {
	requests := make([]url.Values, 0)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Query())
		_, _ = w.Write([]byte(`{"data":{"resultType":"matrix","result":[]}}`))
	}))
	defer server.Close()

	handler := NewGetLogsHandler(server.Client(), testLogsConfig(server.URL))
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetLogsArgs{
		LogjsonQuery: []map[string]interface{}{
			{
				"type": "filter",
				"query": map[string]interface{}{
					"$contains": []interface{}{"Body", "error"},
				},
			},
		},
		StartTimeISO: "1970-01-01T00:00:00Z",
		EndTimeISO:   "1970-01-01T00:07:00Z",
		Limit:        3,
	})
	if err == nil {
		t.Fatal("expected handler to fail for non-stream chunk result")
	}
	if err.Error() != `chunked get_logs expected streams result, got "matrix"` {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(requests) != 1 {
		t.Fatalf("expected a single chunk request, got %d", len(requests))
	}
}

func TestFetchServiceLogsChunksAndHonorsEntryLimit(t *testing.T) {
	requests := make([]url.Values, 0)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Query())

		switch len(requests) {
		case 1:
			_, _ = w.Write([]byte(streamsAPIResponse(
				[]logValue{{Timestamp: "420000000000", Message: "latest"}, {Timestamp: "300000000000", Message: "recent"}},
			)))
		case 2:
			// Deliberately over-return to verify we trim by entry count, not stream count.
			_, _ = w.Write([]byte(streamsAPIResponse(
				[]logValue{{Timestamp: "60000000000", Message: "older"}, {Timestamp: "30000000000", Message: "oldest"}},
			)))
		default:
			t.Fatalf("unexpected extra request %d", len(requests))
		}
	}))
	defer server.Close()

	startTime := time.Unix(60, 0).UTC()
	endTime := startTime.Add(7 * time.Minute)

	response, err := fetchServiceLogs(
		context.Background(),
		server.Client(),
		testLogsConfig(server.URL),
		"api",
		startTime,
		endTime,
		3,
		[]string{"error"},
		[]string{"timeout"},
		"physical_index:payments",
	)
	if err != nil {
		t.Fatalf("fetchServiceLogs returned error: %v", err)
	}

	if len(requests) != 2 {
		t.Fatalf("expected 2 chunk requests, got %d", len(requests))
	}
	if got := requests[0].Get("limit"); got != "3" {
		t.Fatalf("expected first request limit=3, got %q", got)
	}
	if got := requests[1].Get("limit"); got != "1" {
		t.Fatalf("expected second request limit=1, got %q", got)
	}
	if response.Count != 3 {
		t.Fatalf("expected count=3, got %d", response.Count)
	}
	if len(response.Logs) != 3 {
		t.Fatalf("expected 3 logs, got %d", len(response.Logs))
	}
	if response.Logs[2].Message != "older" {
		t.Fatalf("expected final retained log to be trimmed to 'older', got %#v", response.Logs[2])
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
