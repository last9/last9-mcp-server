package traces

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Adaptive sizing rules (mirroring the dashboard's getVolumeQueryChunks):
//   - range ≤ SplitThresholdMs (1h) → exactly 1 chunk covering the full window
//   - range > 1h, no body-parse → range/6 chunks, capped at MaxChunkSizeMs (1h)
//
// Trace pipelines never reference a Body field, so HasExpensiveBodyParsing is
// always false → trace queries always follow the time-range adaptive rules.

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
	rec := newTracesRequestRecorder()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		rec.add(q)
		// 90-min range → 6 chunks of 15 min each (range/6, no body parse).
		switch q.Get("start") + "-" + q.Get("end") {
		case "4500-5400":
			_, _ = w.Write([]byte(traceAPIResponse(2)))
		case "3600-4500", "2700-3600", "1800-2700", "900-1800":
			_, _ = w.Write([]byte(traceAPIResponse(0)))
		case "0-900":
			_, _ = w.Write([]byte(traceAPIResponse(2)))
		default:
			t.Errorf("unexpected chunk %s", q.Encode())
			http.Error(w, "unexpected", http.StatusBadRequest)
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
		EndTimeISO:   "1970-01-01T01:30:00Z",
		Limit:        3,
	})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if got := rec.count(); got != 6 {
		t.Fatalf("expected 6 chunk requests (range/6 sizing), got %d", got)
	}
	for _, q := range rec.all() {
		if got := q.Get("limit"); got != "3" {
			t.Fatalf("expected every chunk limit=3 (effective), got %q", got)
		}
	}

	payload := parseTracesToolResult(t, result)
	if count := countTracesInPayload(t, payload); count != 3 {
		t.Fatalf("expected 3 traces in merged payload, got %d", count)
	}
}

func TestGetTracesHandlerCapsAtConfiguredMax(t *testing.T) {
	rec := newTracesRequestRecorder()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		rec.add(q)
		switch q.Get("start") + "-" + q.Get("end") {
		case "4500-5400":
			_, _ = w.Write([]byte(traceAPIResponse(2)))
		case "3600-4500", "2700-3600", "1800-2700", "900-1800":
			_, _ = w.Write([]byte(traceAPIResponse(0)))
		case "0-900":
			_, _ = w.Write([]byte(traceAPIResponse(2)))
		default:
			t.Errorf("unexpected chunk %s", q.Encode())
			http.Error(w, "unexpected", http.StatusBadRequest)
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
		EndTimeISO:   "1970-01-01T01:30:00Z",
		Limit:        100,
	})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	for _, q := range rec.all() {
		if got := q.Get("limit"); got != "3" {
			t.Fatalf("expected every chunk capped at limit=3, got %q", got)
		}
	}

	payload := parseTracesToolResult(t, result)
	if count := countTracesInPayload(t, payload); count != 3 {
		t.Fatalf("expected 3 traces after cap, got %d", count)
	}
}

func TestGetTracesHandlerSingleChunkForSubThresholdRange(t *testing.T) {
	rec := newTracesRequestRecorder()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.add(r.URL.Query())
		_, _ = w.Write([]byte(traceAPIResponse(1)))
	}))
	defer server.Close()

	handler := NewGetTracesHandler(server.Client(), testChunkTracesConfig(server.URL))
	// 30 min range — below SplitThresholdMs → adaptive returns a single chunk.
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetTracesArgs{
		TracejsonQuery: []map[string]interface{}{
			{"type": "filter", "query": map[string]interface{}{"$exists": []string{"ServiceName"}}},
		},
		StartTimeISO: "1970-01-01T00:00:00Z",
		EndTimeISO:   "1970-01-01T00:30:00Z",
	})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if rec.count() != 1 {
		t.Fatalf("expected 1 chunk for sub-threshold range, got %d", rec.count())
	}
}

func TestGetTracesHandlerEmptyChunks(t *testing.T) {
	rec := newTracesRequestRecorder()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.add(r.URL.Query())
		_, _ = w.Write([]byte(`{"data":{"result":[]}}`))
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
	rec := newTracesRequestRecorder()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.add(r.URL.Query())
		_, _ = w.Write([]byte(traceAPIResponse(1)))
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

	if rec.count() != 1 {
		t.Fatalf("expected 1 request for exact trace ID lookup, got %d", rec.count())
	}
	q := rec.all()[0]
	if got := q.Get("limit"); got != "3" {
		t.Fatalf("expected full-window request limit=3, got %q", got)
	}
	if got := q.Get("start"); got != "0" || q.Get("end") != "1800" {
		t.Fatalf("expected full-window bounds start=0 end=1800, got %v", q)
	}

	payload := parseTracesToolResult(t, result)
	if count := countTracesInPayload(t, payload); count != 1 {
		t.Fatalf("expected 1 trace in payload, got %d", count)
	}
}

func TestGetTracesHandlerHardErrorsWhenAllChunksFail(t *testing.T) {
	// Regression: when every chunk returns an upstream error and no chunk
	// produced a valid response, fetchTraceJSONQuery must surface a hard
	// error rather than an empty partial result. Symmetric to the logs
	// fetchServiceLogs equivalent (TestFetchServiceLogsHardErrorsWhenAllChunksFail).
	rec := newTracesRequestRecorder()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.add(r.URL.Query())
		http.Error(w, "backend exploded", http.StatusInternalServerError)
	}))
	defer server.Close()

	handler := NewGetTracesHandler(server.Client(), testChunkTracesConfig(server.URL))
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetTracesArgs{
		TracejsonQuery: []map[string]interface{}{
			{"type": "filter", "query": map[string]interface{}{"$exists": []string{"ServiceName"}}},
		},
		StartTimeISO: "1970-01-01T00:00:00Z",
		EndTimeISO:   "1970-01-01T01:30:00Z",
		Limit:        10,
	})
	if err != nil {
		t.Fatalf("expected tool execution error, got protocol error: %v", err)
	}
	if result == nil || !result.IsError {
		t.Fatalf("expected IsError=true when every chunk fails, got %#v", result)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	if strings.Contains(text, "backend exploded") {
		t.Fatalf("tool error leaked upstream response body: %s", text)
	}
	if !strings.Contains(text, "500") {
		t.Fatalf("tool error lacks safe status context: %s", text)
	}
	// All 6 chunks (90-min range → 6 chunks of 15 min each) should have been
	// attempted in parallel; failure of one chunk doesn't short-circuit the
	// others.
	if rec.count() != 6 {
		t.Fatalf("expected 6 chunk attempts even with all failing, got %d", rec.count())
	}
}

func TestGetTracesHandlerReturnsPartialResultAfterLaterChunkError(t *testing.T) {
	rec := newTracesRequestRecorder()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		rec.add(q)
		switch q.Get("start") + "-" + q.Get("end") {
		case "4500-5400":
			_, _ = w.Write([]byte(traceAPIResponse(2)))
		case "3600-4500", "2700-3600", "1800-2700":
			_, _ = w.Write([]byte(traceAPIResponse(0)))
		case "900-1800":
			_, _ = w.Write([]byte(traceAPIResponse(1)))
		case "0-900":
			http.Error(w, "backend error", http.StatusInternalServerError)
		default:
			t.Errorf("unexpected chunk %s", q.Encode())
			http.Error(w, "unexpected", http.StatusBadRequest)
		}
	}))
	defer server.Close()

	handler := NewGetTracesHandler(server.Client(), testChunkTracesConfig(server.URL))
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetTracesArgs{
		TracejsonQuery: []map[string]interface{}{
			{"type": "filter", "query": map[string]interface{}{"$exists": []string{"ServiceName"}}},
		},
		StartTimeISO: "1970-01-01T00:00:00Z",
		EndTimeISO:   "1970-01-01T01:30:00Z",
		Limit:        10,
	})
	if err != nil {
		t.Fatalf("handler returned error on partial: %v", err)
	}

	if rec.count() != 6 {
		t.Fatalf("expected 6 chunk requests, got %d", rec.count())
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
	if !ok || !strings.Contains(warning, "chunk 6/6 failed") {
		t.Fatalf("expected chunk 6/6 failure in warning, got %q", warning)
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
		{name: "no limit requested uses tool default", cfg: models.Config{}, requested: 0, want: models.DefaultMaxGetTracesEntries},
		{name: "limit below max is honoured", cfg: models.Config{}, requested: 50, want: 50},
		{name: "limit above default max is capped", cfg: models.Config{}, requested: 9999999, want: models.DefaultMaxGetTracesEntries},
		{name: "configured max overrides default", cfg: models.Config{MaxGetTracesEntries: 10}, requested: 0, want: 10},
		{name: "request above configured max is capped", cfg: models.Config{MaxGetTracesEntries: 100}, requested: 200, want: 100},
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

// --- helpers ---

type tracesRequestRecorder struct {
	mu   sync.Mutex
	reqs []url.Values
}

func newTracesRequestRecorder() *tracesRequestRecorder {
	return &tracesRequestRecorder{}
}

func (r *tracesRequestRecorder) add(q url.Values) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reqs = append(r.reqs, q)
}

func (r *tracesRequestRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.reqs)
}

func (r *tracesRequestRecorder) all() []url.Values {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]url.Values, len(r.reqs))
	copy(out, r.reqs)
	return out
}

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
