package traces

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// testTracesConfig builds a minimal Config pointing at the given server URL.
func testChunkTracesConfig(serverURL string) models.Config {
	return models.Config{
		APIBaseURL: serverURL,
		Region:     "ap-south-1",
		TokenManager: &auth.TokenManager{
			AccessToken: "mock-token",
			ExpiresAt:   time.Now().Add(365 * 24 * time.Hour),
		},
	}
}

// traceAPIResponse builds a minimal traces API response containing n trace items.
func traceAPIResponse(n int) string {
	items := make([]map[string]interface{}, 0, n)
	for i := 0; i < n; i++ {
		items = append(items, map[string]interface{}{
			"TraceId":     fmt.Sprintf("trace-%d", i),
			"ServiceName": "test-service",
			"Duration":    int64(100000),
			"Timestamp":   "2025-01-01T00:00:00Z",
		})
	}
	body, _ := json.Marshal(map[string]interface{}{
		"data": map[string]interface{}{
			"result": items,
		},
	})
	return string(body)
}

func TestGetTracesHandlerChunksAndHonorsLimit(t *testing.T) {
	requests := make([]url.Values, 0)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Query())

		switch len(requests) {
		case 1:
			// newest chunk — returns 2 traces
			w.Write([]byte(traceAPIResponse(2)))
		case 2:
			// older chunk — returns 2 more traces (only 1 should be kept due to limit=3)
			w.Write([]byte(traceAPIResponse(2)))
		default:
			t.Fatalf("unexpected extra request %d", len(requests))
		}
	}))
	defer server.Close()

	handler := NewGetTracesHandler(server.Client(), testChunkTracesConfig(server.URL))
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetTracesArgs{
		TracejsonQuery: []map[string]interface{}{
			{
				"type":  "filter",
				"query": map[string]interface{}{"$exists": []string{"ServiceName"}},
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

	// First chunk limit should be 3
	if got := requests[0].Get("limit"); got != "3" {
		t.Fatalf("expected first chunk limit=3, got %q", got)
	}
	// Second chunk limit should be remaining = 3 - 2 = 1
	if got := requests[1].Get("limit"); got != "1" {
		t.Fatalf("expected second chunk limit=1, got %q", got)
	}

	// Verify time windows: 7-minute range → chunks [2min–7min], [0–2min] (seconds)
	if got := requests[0].Get("start"); got != "120" || requests[0].Get("end") != "420" {
		t.Fatalf("unexpected first chunk bounds: %v", requests[0])
	}
	if got := requests[1].Get("start"); got != "0" || requests[1].Get("end") != "120" {
		t.Fatalf("unexpected second chunk bounds: %v", requests[1])
	}

	// Result should contain exactly 3 traces (2 from chunk 1 + 1 trimmed from chunk 2)
	payload := parseTracesToolResult(t, result)
	if count := countTracesInPayload(t, payload); count != 3 {
		t.Fatalf("expected 3 traces in merged payload, got %d", count)
	}
}

func TestGetTracesHandlerCapsAtConfiguredMax(t *testing.T) {
	requests := make([]url.Values, 0)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Query())
		switch len(requests) {
		case 1:
			w.Write([]byte(traceAPIResponse(2)))
		case 2:
			w.Write([]byte(traceAPIResponse(2)))
		default:
			t.Fatalf("unexpected extra request %d", len(requests))
		}
	}))
	defer server.Close()

	cfg := testChunkTracesConfig(server.URL)
	cfg.MaxGetTracesEntries = 3

	handler := NewGetTracesHandler(server.Client(), cfg)
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetTracesArgs{
		TracejsonQuery: []map[string]interface{}{
			{"type": "filter", "query": map[string]interface{}{"$exists": []string{"ServiceName"}}},
		},
		StartTimeISO: "1970-01-01T00:00:00Z",
		EndTimeISO:   "1970-01-01T00:07:00Z",
		Limit:        100, // requested more than max
	})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if got := requests[0].Get("limit"); got != "3" {
		t.Fatalf("expected first chunk limit capped to 3, got %q", got)
	}

	payload := parseTracesToolResult(t, result)
	if count := countTracesInPayload(t, payload); count != 3 {
		t.Fatalf("expected 3 traces after cap, got %d", count)
	}
}

func TestGetTracesHandlerEmitsChunkingDebugLogs(t *testing.T) {
	t.Setenv(tracesChunkingDebugEnvVar, "true")

	var logBuffer bytes.Buffer
	originalWriter := log.Writer()
	originalFlags := log.Flags()
	log.SetOutput(&logBuffer)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(originalWriter)
		log.SetFlags(originalFlags)
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("start") + "-" + r.URL.Query().Get("end") {
		case "120-420":
			w.Write([]byte(traceAPIResponse(2)))
		case "0-120":
			w.Write([]byte(traceAPIResponse(2)))
		default:
			t.Fatalf("unexpected chunk request %q", r.URL.RawQuery)
		}
	}))
	defer server.Close()

	cfg := testChunkTracesConfig(server.URL)
	cfg.MaxGetTracesEntries = 3

	handler := NewGetTracesHandler(server.Client(), cfg)
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetTracesArgs{
		TracejsonQuery: []map[string]interface{}{
			{"type": "filter", "query": map[string]interface{}{"$exists": []string{"ServiceName"}}},
		},
		StartTimeISO: "1970-01-01T00:00:00Z",
		EndTimeISO:   "1970-01-01T00:07:00Z",
		Limit:        100,
	})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	output := logBuffer.String()
	for _, want := range []string{
		"[chunking] get_traces chunking enabled chunks=2 start_ms=0 end_ms=420000 requested_limit=100 effective_limit=3",
		"[chunking] get_traces requested limit capped requested_limit=100 configured_max=3",
		"[chunking] get_traces chunk request chunk=1/2 start_ms=120000 end_ms=420000 chunk_limit=3 remaining_limit=3",
		"[chunking] get_traces chunk response chunk=1/2 returned_traces=2",
		"[chunking] get_traces chunk merged chunk=1/2 merged_traces=2 remaining_limit=1",
		"[chunking] get_traces chunk request chunk=2/2 start_ms=0 end_ms=120000 chunk_limit=1 remaining_limit=1",
		"[chunking] get_traces chunk trim chunk=2/2 kept_traces=1 dropped_traces=1",
		"[chunking] get_traces chunking complete chunks=2 returned_traces=3 start_ms=0 end_ms=420000",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected debug log %q in output:\n%s", want, output)
		}
	}
}

func TestGetTracesHandlerEmptyChunks(t *testing.T) {
	requests := make([]url.Values, 0)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Query())
		// All chunks return empty
		w.Write([]byte(`{"data":{"result":[]}}`))
	}))
	defer server.Close()

	handler := NewGetTracesHandler(server.Client(), testChunkTracesConfig(server.URL))
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetTracesArgs{
		TracejsonQuery: []map[string]interface{}{
			{"type": "filter", "query": map[string]interface{}{"$exists": []string{"ServiceName"}}},
		},
		StartTimeISO: "1970-01-01T00:00:00Z",
		EndTimeISO:   "1970-01-01T00:07:00Z",
	})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	payload := parseTracesToolResult(t, result)
	if count := countTracesInPayload(t, payload); count != 0 {
		t.Fatalf("expected 0 traces in empty response, got %d", count)
	}
}

func TestGetTracesHandlerExactTraceIDUsesSingleRequest(t *testing.T) {
	requests := make([]url.Values, 0)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Query())
		if len(requests) > 1 {
			t.Fatalf("unexpected extra request %d", len(requests))
		}
		w.Write([]byte(traceAPIResponse(1)))
	}))
	defer server.Close()

	handler := NewGetTracesHandler(server.Client(), testChunkTracesConfig(server.URL))
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetTracesArgs{
		TracejsonQuery: []map[string]interface{}{
			{
				"type": "filter",
				"query": map[string]interface{}{
					"$and": []interface{}{
						map[string]interface{}{
							"$eq": []interface{}{"TraceId", "ea8148dece205073096e4ad48145b08a"},
						},
					},
				},
			},
		},
		StartTimeISO: "1970-01-01T00:00:00Z",
		EndTimeISO:   "1970-01-01T00:30:00Z",
		Limit:        3,
	})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if len(requests) != 1 {
		t.Fatalf("expected 1 request for exact trace ID lookup, got %d", len(requests))
	}
	if got := requests[0].Get("limit"); got != "3" {
		t.Fatalf("expected full-window request limit=3, got %q", got)
	}
	if got := requests[0].Get("start"); got != "0" || requests[0].Get("end") != "1800" {
		t.Fatalf("expected full-window bounds start=0 end=1800, got %v", requests[0])
	}

	payload := parseTracesToolResult(t, result)
	if count := countTracesInPayload(t, payload); count != 1 {
		t.Fatalf("expected 1 trace in payload, got %d", count)
	}
}

func TestGetTracesHandlerReturnsPartialResultAfterLaterChunkError(t *testing.T) {
	requests := make([]url.Values, 0)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Query())
		switch len(requests) {
		case 1:
			w.Write([]byte(traceAPIResponse(2)))
		case 2:
			w.Write([]byte(traceAPIResponse(1)))
		case 3:
			http.Error(w, "backend error", http.StatusInternalServerError)
		default:
			t.Fatalf("unexpected extra request %d", len(requests))
		}
	}))
	defer server.Close()

	handler := NewGetTracesHandler(server.Client(), testChunkTracesConfig(server.URL))
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetTracesArgs{
		TracejsonQuery: []map[string]interface{}{
			{"type": "filter", "query": map[string]interface{}{"$exists": []string{"ServiceName"}}},
		},
		StartTimeISO: "1970-01-01T00:00:00Z",
		EndTimeISO:   "1970-01-01T00:13:00Z",
		Limit:        10,
	})
	if err != nil {
		t.Fatalf("handler returned error on partial: %v", err)
	}

	if len(requests) != 3 {
		t.Fatalf("expected 3 chunk requests, got %d", len(requests))
	}

	payload := parseTracesToolResult(t, result)
	if count := countTracesInPayload(t, payload); count != 3 {
		t.Fatalf("expected 3 traces in partial result, got %d", count)
	}

	meta, ok := payload[partialResultMetadataKey].(map[string]interface{})
	if !ok {
		t.Fatalf("expected partial metadata in payload, got %#v", payload[partialResultMetadataKey])
	}
	if partial, ok := meta["partial_result"].(bool); !ok || !partial {
		t.Fatalf("expected partial_result=true, got %#v", meta["partial_result"])
	}
	warning, ok := meta["warning"].(string)
	if !ok || !strings.Contains(warning, "chunk 3/3 failed") {
		t.Fatalf("expected chunk failure in warning, got %q", warning)
	}
}

func TestParseTimeRangeFromArgsAtDefaultsToSixtyMinutes(t *testing.T) {
	now := time.Date(2026, time.March, 17, 12, 0, 0, 0, time.UTC)

	startMs, endMs, err := parseTimeRangeFromArgsAt(GetTracesArgs{}, now)
	if err != nil {
		t.Fatalf("parseTimeRangeFromArgsAt returned error: %v", err)
	}

	duration := time.UnixMilli(endMs).Sub(time.UnixMilli(startMs))
	if duration != 60*time.Minute {
		t.Fatalf("expected 60-minute default lookback, got %s", duration)
	}
	if got := time.UnixMilli(endMs).UTC(); !got.Equal(now) {
		t.Fatalf("expected end time %s, got %s", now, got)
	}
}

func TestEffectiveGetTracesLimit(t *testing.T) {
	tests := []struct {
		name      string
		cfg       models.Config
		requested int
		want      int
	}{
		{
			name:      "no limit requested uses tool default",
			cfg:       models.Config{},
			requested: 0,
			want:      models.DefaultMaxGetTracesEntries,
		},
		{
			name:      "limit below max is honoured",
			cfg:       models.Config{},
			requested: 50,
			want:      50,
		},
		{
			name:      "limit above default max is capped",
			cfg:       models.Config{},
			requested: 9999999,
			want:      models.DefaultMaxGetTracesEntries,
		},
		{
			name:      "configured max overrides default",
			cfg:       models.Config{MaxGetTracesEntries: 10},
			requested: 0,
			want:      10,
		},
		{
			name:      "request above configured max is capped",
			cfg:       models.Config{MaxGetTracesEntries: 100},
			requested: 200,
			want:      100,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := effectiveGetTracesLimit(tt.cfg, tt.requested)
			if got != tt.want {
				t.Errorf("effectiveGetTracesLimit() = %d, want %d", got, tt.want)
			}
		})
	}
}

// helpers

func parseTracesToolResult(t *testing.T, result *mcp.CallToolResult) map[string]interface{} {
	t.Helper()
	if len(result.Content) == 0 {
		t.Fatal("expected tool content")
	}
	text, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result.Content[0])
	}
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(text.Text), &payload); err != nil {
		t.Fatalf("failed to parse result JSON: %v", err)
	}
	return payload
}

func countTracesInPayload(t *testing.T, payload map[string]interface{}) int {
	t.Helper()
	data, ok := payload["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("missing data object: %#v", payload)
	}
	result, ok := data["result"].([]interface{})
	if !ok {
		t.Fatalf("missing result array: %#v", data)
	}
	return len(result)
}
