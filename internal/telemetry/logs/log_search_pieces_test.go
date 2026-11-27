package logs

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"last9-mcp/internal/utils"
)

type logSearchPieceServer struct {
	*httptest.Server
	mu        sync.Mutex
	calls     int
	failStart int64
}

func newLogSearchPieceServer(t *testing.T, failStart int64) *logSearchPieceServer {
	t.Helper()
	s := &logSearchPieceServer{failStart: failStart}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		start, _ := body["start"].(float64)
		s.mu.Lock()
		s.calls++
		s.mu.Unlock()
		if s.failStart != 0 && int64(start) == s.failStart {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"piece unavailable"}`))
			return
		}
		_, _ = fmt.Fprintf(w, `{"query_result":{"status":"success","data":{"resultType":"streams","result":[{"stream":{"service":"checkout"},"values":[["%d","line"]]}]}},"total_matching_lines":1,"logs_truncated":false}`,
			int64(start)*int64(time.Second))
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *logSearchPieceServer) requests() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

type wantPiece struct {
	start, end time.Time
	status     string
}

func assertLogSearchPieces(t *testing.T, stats map[string]any, want []wantPiece) {
	t.Helper()
	pieces, ok := stats["pieces"].([]any)
	if !ok || len(pieces) != len(want) {
		t.Fatalf("search_stats.pieces = %#v, want %d pieces", stats["pieces"], len(want))
	}
	for i, raw := range pieces {
		piece := raw.(map[string]any)
		if piece["start"] != want[i].start.Format(time.RFC3339Nano) ||
			piece["end"] != want[i].end.Format(time.RFC3339Nano) ||
			piece["status"] != want[i].status {
			t.Errorf("piece %d = %#v, want start=%s end=%s status=%s", i, piece,
				want[i].start.Format(time.RFC3339Nano), want[i].end.Format(time.RFC3339Nano), want[i].status)
		}
	}
}

const logSearchWindowLimit = 336 * time.Hour

func TestFetchLogJSONQuery_APIMode_AtWindowLimitIsOneRead(t *testing.T) {
	srv := newLogSearchPieceServer(t, 0)
	end := time.Unix(1_800_000_000, 0).UTC()

	result, _, err := fetchLogJSONQuery(context.Background(), srv.Client(), apiModeCfg(srv.URL),
		[]map[string]interface{}{{"type": "filter"}}, end.Add(-logSearchWindowLimit).UnixMilli(), end.UnixMilli(), GetLogsArgs{Limit: 10})
	if err != nil {
		t.Fatalf("fetchLogJSONQuery: %v", err)
	}
	if got := srv.requests(); got != 1 {
		t.Fatalf("requests = %d, want 1 for a window of exactly 336h", got)
	}
	if stats, _ := result["search_stats"].(map[string]any); stats["pieces"] != nil {
		t.Errorf("search_stats.pieces = %#v, want absent for a single read", stats["pieces"])
	}
}

func TestFetchLogJSONQuery_APIMode_OneSecondOverLimitReadsTwoPieces(t *testing.T) {
	srv := newLogSearchPieceServer(t, 0)
	end := time.Unix(1_800_000_000, 0).UTC()
	start := end.Add(-logSearchWindowLimit - time.Second)

	result, _, err := fetchLogJSONQuery(context.Background(), srv.Client(), apiModeCfg(srv.URL),
		[]map[string]interface{}{{"type": "filter"}}, start.UnixMilli(), end.UnixMilli(), GetLogsArgs{Limit: 10})
	if err != nil {
		t.Fatalf("fetchLogJSONQuery: %v", err)
	}
	if got := srv.requests(); got != 2 {
		t.Fatalf("requests = %d, want 2", got)
	}
	assertLogSearchPieces(t, result["search_stats"].(map[string]any), []wantPiece{
		{start: end.Add(-logSearchWindowLimit), end: end, status: "complete"},
		{start: start, end: end.Add(-logSearchWindowLimit), status: "complete"},
	})
}

func TestFetchLogJSONQuery_APIMode_ThirtyDaysReadsThreeContiguousPieces(t *testing.T) {
	srv := newLogSearchPieceServer(t, 0)
	end := time.Unix(1_800_000_000, 0).UTC()
	start := end.Add(-30 * 24 * time.Hour)

	result, _, err := fetchLogJSONQuery(context.Background(), srv.Client(), apiModeCfg(srv.URL),
		[]map[string]interface{}{{"type": "filter"}}, start.UnixMilli(), end.UnixMilli(), GetLogsArgs{Limit: 2})
	if err != nil {
		t.Fatalf("fetchLogJSONQuery: %v", err)
	}
	if got := srv.requests(); got != 3 {
		t.Fatalf("requests = %d, want 3", got)
	}
	// Each piece returns one line, so the limit of 2 drops the oldest piece's row.
	assertLogSearchPieces(t, result["search_stats"].(map[string]any), []wantPiece{
		{start: end.Add(-logSearchWindowLimit), end: end, status: "complete"},
		{start: end.Add(-2 * logSearchWindowLimit), end: end.Add(-logSearchWindowLimit), status: "complete"},
		{start: start, end: end.Add(-2 * logSearchWindowLimit), status: "partial"},
	})
	if got := countLogEntriesInResultItems(result["data"].(map[string]any)["result"].([]any)); got != 2 {
		t.Errorf("returned lines = %d, want limit 2", got)
	}
}

func TestFetchServiceLogs_APIMode_FailedPieceReportedAsUncovered(t *testing.T) {
	end := time.Unix(1_800_000_000, 0).UTC()
	start := end.Add(-30 * 24 * time.Hour)
	middleStart := end.Add(-2 * logSearchWindowLimit)
	srv := newLogSearchPieceServer(t, middleStart.Unix())

	got, err := fetchServiceLogs(context.Background(), srv.Client(), apiModeCfg(srv.URL), "checkout",
		start, end, 20, serviceLogsPipeline(), "")
	if err != nil {
		t.Fatalf("fetchServiceLogs: %v", err)
	}
	assertLogSearchPieces(t, got.SearchStats, []wantPiece{
		{start: end.Add(-logSearchWindowLimit), end: end, status: "complete"},
		{start: middleStart, end: end.Add(-logSearchWindowLimit), status: "failed"},
		{start: start, end: middleStart, status: "complete"},
	})
	failed := got.SearchStats["pieces"].([]any)[1].(map[string]any)
	if msg, _ := failed["error"].(string); msg == "" {
		t.Errorf("failed piece = %#v, want its error", failed)
	}
	if got.LogsTruncated == nil || !*got.LogsTruncated {
		t.Errorf("logs_truncated = %v, want true when a piece failed", got.LogsTruncated)
	}
}

func TestMergeLogSearchWindows_UpstreamTruncatedPieceIsPartial(t *testing.T) {
	piece := func(truncated bool) map[string]any {
		return map[string]any{
			"data":           map[string]any{"resultType": "streams", "result": []any{}},
			"logs_truncated": truncated,
		}
	}
	results := []utils.ChunkResult[map[string]any]{
		{Chunk: utils.TimeChunk{StartMs: 2000, EndMs: 3000}, Value: piece(false)},
		{Chunk: utils.TimeChunk{StartMs: 1000, EndMs: 2000}, Index: 1, Value: piece(true)},
	}

	result, err := mergeLogSearchWindows(results, 100)
	if err != nil {
		t.Fatalf("mergeLogSearchWindows: %v", err)
	}
	assertLogSearchPieces(t, result["search_stats"].(map[string]any), []wantPiece{
		{start: time.UnixMilli(2000).UTC(), end: time.UnixMilli(3000).UTC(), status: "complete"},
		{start: time.UnixMilli(1000).UTC(), end: time.UnixMilli(2000).UTC(), status: "partial"},
	})
}

func TestMergeLogSearchWindows_EmptyPieceAfterLimitRemainsComplete(t *testing.T) {
	piece := func(entries int) map[string]any {
		values := make([]any, entries)
		for i := range values {
			values[i] = []any{fmt.Sprint(i), "line"}
		}
		result := []any{}
		if entries > 0 {
			result = append(result, map[string]any{"stream": map[string]any{"service": "checkout"}, "values": values})
		}
		return map[string]any{
			"data":           map[string]any{"resultType": "streams", "result": result},
			"logs_truncated": false,
		}
	}
	results := []utils.ChunkResult[map[string]any]{
		{Chunk: utils.TimeChunk{StartMs: 3000, EndMs: 4000}, Value: piece(7)},
		{Chunk: utils.TimeChunk{StartMs: 2000, EndMs: 3000}, Index: 1, Value: piece(7)},
		{Chunk: utils.TimeChunk{StartMs: 1000, EndMs: 2000}, Index: 2, Value: piece(0)},
	}

	result, err := mergeLogSearchWindows(results, 10)
	if err != nil {
		t.Fatalf("mergeLogSearchWindows: %v", err)
	}
	assertLogSearchPieces(t, result["search_stats"].(map[string]any), []wantPiece{
		{start: time.UnixMilli(3000).UTC(), end: time.UnixMilli(4000).UTC(), status: "complete"},
		{start: time.UnixMilli(2000).UTC(), end: time.UnixMilli(3000).UTC(), status: "partial"},
		{start: time.UnixMilli(1000).UTC(), end: time.UnixMilli(2000).UTC(), status: "complete"},
	})
}

func TestFetchLogJSONQuery_APIMode_AggregateOverWindowLimitIsRefused(t *testing.T) {
	end := time.Unix(1_800_000_000, 0).UTC()
	aggregate := []map[string]interface{}{{"type": "aggregate"}}
	for _, tc := range []struct {
		name      string
		window    time.Duration
		wantCalls int
	}{
		{name: "at limit", window: logSearchWindowLimit, wantCalls: 1},
		{name: "limit plus one second", window: logSearchWindowLimit + time.Second, wantCalls: 0},
		{name: "thirty days", window: 30 * 24 * time.Hour, wantCalls: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := newLogSearchPieceServer(t, 0)
			_, _, err := fetchLogJSONQuery(context.Background(), srv.Client(), apiModeCfg(srv.URL), aggregate,
				end.Add(-tc.window).UnixMilli(), end.UnixMilli(), GetLogsArgs{Limit: 10})
			if got := srv.requests(); got != tc.wantCalls {
				t.Errorf("requests = %d, want %d", got, tc.wantCalls)
			}
			if tc.wantCalls == 0 && (err == nil || !strings.Contains(err.Error(), "336 hours")) {
				t.Errorf("err = %v, want a refusal naming the 336 hours limit", err)
			}
		})
	}
}
