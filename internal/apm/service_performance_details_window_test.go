package apm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/constants"
	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func perfDetailsTestConfig(serverURL string) models.Config {
	return models.Config{
		APIBaseURL: serverURL,
		Region:     "ap-south-1",
		OrgSlug:    "test-org",
		ClusterID:  "test-cluster",
		TokenManager: &auth.TokenManager{
			AccessToken: "mock-token",
			ExpiresAt:   time.Now().Add(365 * 24 * time.Hour),
		},
	}
}

// --- splitIntoPerfDetailsChunks ---

func TestSplitIntoPerfDetailsChunks_NoSplitUnder35Days(t *testing.T) {
	start := int64(0)
	end := int64(10 * 24 * 3600) // 10 days
	chunks := splitIntoPerfDetailsChunks(start, end)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk for a 10 day window, got %d: %+v", len(chunks), chunks)
	}
	if chunks[0].start != start || chunks[0].end != end {
		t.Fatalf("expected chunk to cover the full window, got %+v", chunks[0])
	}
}

func TestSplitIntoPerfDetailsChunks_SplitsWiderWindows(t *testing.T) {
	start := int64(0)
	end := int64(90 * 24 * 3600) // 90 days
	chunks := splitIntoPerfDetailsChunks(start, end)

	const maxChunkSeconds = int64(perfDetailsMaxChunkDays) * 24 * 3600
	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks for a 90 day window (ceil(90/35)), got %d: %+v", len(chunks), chunks)
	}
	for i, c := range chunks {
		if c.end-c.start > maxChunkSeconds {
			t.Errorf("chunk %d width %ds exceeds max %ds", i, c.end-c.start, maxChunkSeconds)
		}
	}
	// Boundaries must be contiguous and inclusive-shared (chunk N's end == chunk N+1's start).
	if chunks[0].start != start {
		t.Errorf("first chunk should start at window start, got %d", chunks[0].start)
	}
	if chunks[len(chunks)-1].end != end {
		t.Errorf("last chunk should end at window end, got %d", chunks[len(chunks)-1].end)
	}
	for i := 1; i < len(chunks); i++ {
		if chunks[i-1].end != chunks[i].start {
			t.Errorf("expected chunk %d end (%d) to equal chunk %d start (%d)", i-1, chunks[i-1].end, i, chunks[i].start)
		}
	}
	// Chunks must be as-close-to-equal-width as possible (all widths within
	// 1 second of each other), not "as many full 35-day chunks as fit, then
	// a narrower remainder" — a 90 day window should split into 3 nearly
	// equal ~30-day chunks, not 35+35+20.
	minWidth, maxWidth := chunks[0].end-chunks[0].start, chunks[0].end-chunks[0].start
	for _, c := range chunks {
		w := c.end - c.start
		if w < minWidth {
			minWidth = w
		}
		if w > maxWidth {
			maxWidth = w
		}
	}
	if maxWidth-minWidth > 1 {
		t.Errorf("expected all chunk widths within 1 second of each other, got widths %v", func() []int64 {
			ws := make([]int64, len(chunks))
			for i, c := range chunks {
				ws[i] = c.end - c.start
			}
			return ws
		}())
	}
}

// --- chunkWindowSelector ---

// splitIntoPerfDetailsChunks currently never produces a chunk narrower than
// 60 seconds, so the 1-minute clamp inside chunkWindowSelector is only
// reachable by calling it directly. Without this test, a future change to
// the splitter could silently regress the "[0m]" invalid-selector fix with
// nothing catching it.
func TestChunkWindowSelector_ClampsSubMinuteWidthTo1m(t *testing.T) {
	c := perfDetailsChunk{start: 1000, end: 1030} // 30s wide, well under 1 minute
	got := chunkWindowSelector(c)
	if got != "1m" {
		t.Errorf("expected a sub-60s-wide chunk to clamp to \"1m\", got %q", got)
	}
}

func TestChunkWindowSelector_NoClampAboveOneMinute(t *testing.T) {
	c := perfDetailsChunk{start: 0, end: 180} // 3 minutes wide
	got := chunkWindowSelector(c)
	if got != "3m" {
		t.Errorf("expected a 3-minute-wide chunk to render as \"3m\" unclamped, got %q", got)
	}
}

// --- mergeChunkedSeries ---

func TestMergeChunkedSeries_DedupsBoundaryAndHandlesPartialLabelSets(t *testing.T) {
	chunk1 := []TimeSeries{
		{
			Metric: map[string]string{"http_status_code": "200"},
			Values: []TimeSeriesPoint{{Timestamp: 100, Value: 1}, {Timestamp: 200, Value: 2}},
		},
	}
	chunk2 := []TimeSeries{
		// Shared boundary timestamp (200) with chunk1's last point - must be deduped.
		{
			Metric: map[string]string{"http_status_code": "200"},
			Values: []TimeSeriesPoint{{Timestamp: 200, Value: 2}, {Timestamp: 300, Value: 3}},
		},
		// A label combination that only appears in chunk2.
		{
			Metric: map[string]string{"http_status_code": "500"},
			Values: []TimeSeriesPoint{{Timestamp: 300, Value: 9}},
		},
	}

	merged := mergeChunkedSeries([][]TimeSeries{chunk1, chunk2})

	if len(merged) != 2 {
		t.Fatalf("expected 2 merged series (200 and 500), got %d: %+v", len(merged), merged)
	}

	var got200, got500 *TimeSeries
	for i := range merged {
		switch merged[i].Metric["http_status_code"] {
		case "200":
			got200 = &merged[i]
		case "500":
			got500 = &merged[i]
		}
	}

	if got200 == nil {
		t.Fatal("expected a merged series for http_status_code=200")
	}
	wantValues := []TimeSeriesPoint{{Timestamp: 100, Value: 1}, {Timestamp: 200, Value: 2}, {Timestamp: 300, Value: 3}}
	if !reflect.DeepEqual(got200.Values, wantValues) {
		t.Errorf("expected deduped boundary values %+v, got %+v", wantValues, got200.Values)
	}

	if got500 == nil {
		t.Fatal("expected label combination present only in chunk2 to still show up in the merge")
	}
	if len(got500.Values) != 1 || got500.Values[0].Value != 9 {
		t.Errorf("unexpected values for the chunk2-only series: %+v", got500.Values)
	}
}

// TestMergeChunkedSeries_MisalignedGridBoundary_NoDuplicatesMonotonic covers
// the case exact-timestamp-equality dedup misses: adjacent chunks whose
// backend-derived output steps land on different-resolution time grids, so
// chunk 2's "first" point can sit BEFORE chunk 1's last point rather than
// exactly equal to it.
func TestMergeChunkedSeries_MisalignedGridBoundary_NoDuplicatesMonotonic(t *testing.T) {
	// Chunk 1 (fine grid) ends at T=1000.
	chunk1 := []TimeSeries{
		{
			Metric: map[string]string{"http_status_code": "200"},
			Values: []TimeSeriesPoint{
				{Timestamp: 900, Value: 1},
				{Timestamp: 1000, Value: 2},
			},
		},
	}
	// Chunk 2 (coarser grid) starts at T=970 — BEFORE chunk1's last point —
	// and continues past it.
	chunk2 := []TimeSeries{
		{
			Metric: map[string]string{"http_status_code": "200"},
			Values: []TimeSeriesPoint{
				{Timestamp: 970, Value: 5},
				{Timestamp: 1100, Value: 6},
			},
		},
	}

	merged := mergeChunkedSeries([][]TimeSeries{chunk1, chunk2})
	if len(merged) != 1 {
		t.Fatalf("expected 1 merged series, got %d: %+v", len(merged), merged)
	}

	got := merged[0].Values
	wantValues := []TimeSeriesPoint{
		{Timestamp: 900, Value: 1},
		{Timestamp: 1000, Value: 2},
		{Timestamp: 1100, Value: 6},
	}
	if !reflect.DeepEqual(got, wantValues) {
		t.Errorf("expected chunk2's stale (<=last-appended-timestamp) point dropped and the rest kept, want %+v, got %+v", wantValues, got)
	}
	for i := 1; i < len(got); i++ {
		if got[i].Timestamp <= got[i-1].Timestamp {
			t.Errorf("expected strictly increasing timestamps, got %+v", got)
		}
	}
}

// --- mergeTopFloat / mergeTopInt64 ---

func TestMergeTopFloat_MaxPerKeySortedTruncated(t *testing.T) {
	chunkResults := [][]map[string]float64{
		{{"op-a": 10}, {"op-b": 5}},
		{{"op-a": 20}, {"op-c": 30}, {"op-d": 1}},
	}
	got := mergeTopFloat(chunkResults, 3)
	if len(got) != 3 {
		t.Fatalf("expected result truncated to 3 entries, got %d: %+v", len(got), got)
	}
	want := []map[string]float64{{"op-c": 30}, {"op-a": 20}, {"op-b": 5}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("expected %+v (max per key, desc sorted, truncated), got %+v", want, got)
	}
}

func TestMergeTopInt64_SumsPerKeySortedTruncated(t *testing.T) {
	// Counts must be SUMMED across chunks, not maxed: an error occurring 5
	// times in chunk 1 and 3 times in chunk 2 totals 8.
	chunkResults := [][]map[string]int64{
		{{"500": 5}, {"404": 100}},
		{{"500": 3}},
	}
	got := mergeTopInt64(chunkResults, 10)
	// "500": 5+3=8 summed; "404": 100 (only in one chunk). Descending by total.
	want := []map[string]int64{{"404": 100}, {"500": 8}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("expected %+v, got %+v", want, got)
	}
}

// --- handler-level behavior ---

func TestServicePerformanceDetails_RejectsWindowOverMaxDays(t *testing.T) {
	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("[]"))
	}))
	defer server.Close()

	handler := NewServicePerformanceDetailsHandler(server.Client(), perfDetailsTestConfig(server.URL))

	now := time.Now().UTC()
	args := ServicePerformanceDetailsArgs{
		ServiceName:  "svc",
		StartTimeISO: now.Add(-400 * 24 * time.Hour).Format(time.RFC3339),
		EndTimeISO:   now.Format(time.RFC3339),
		Env:          "prod",
	}

	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, args)
	if err == nil {
		t.Fatal("expected an error for a >366 day window, got nil")
	}
	if !strings.Contains(err.Error(), "time range too wide") {
		t.Errorf("unexpected error message: %v", err)
	}
	if rc := requestCount.Load(); rc != 0 {
		t.Errorf("expected no HTTP calls for a rejected window, got %d", rc)
	}
}

func TestServicePerformanceDetails_NoChunkingUnder35Days(t *testing.T) {
	var rangeCalls, instantCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case constants.EndpointPromQuery:
			rangeCalls.Add(1)
		case constants.EndpointPromQueryInstant:
			instantCalls.Add(1)
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("[]"))
	}))
	defer server.Close()

	handler := NewServicePerformanceDetailsHandler(server.Client(), perfDetailsTestConfig(server.URL))

	now := time.Now().UTC()
	args := ServicePerformanceDetailsArgs{
		ServiceName:  "svc",
		StartTimeISO: now.Add(-10 * 24 * time.Hour).Format(time.RFC3339),
		EndTimeISO:   now.Format(time.RFC3339),
		Env:          "prod",
	}

	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, args)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	// 1 chunk * 6 range-vector sub-queries, and 1 chunk * 3 instant sub-queries.
	if got := rangeCalls.Load(); got != 6 {
		t.Errorf("expected 6 range-query calls (no chunking), got %d", got)
	}
	if got := instantCalls.Load(); got != 3 {
		t.Errorf("expected 3 instant-query calls (no chunking), got %d", got)
	}
}

func TestServicePerformanceDetails_SplitsWiderWindowIntoChunks(t *testing.T) {
	var rangeCalls, instantCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case constants.EndpointPromQuery:
			rangeCalls.Add(1)
		case constants.EndpointPromQueryInstant:
			instantCalls.Add(1)
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("[]"))
	}))
	defer server.Close()

	handler := NewServicePerformanceDetailsHandler(server.Client(), perfDetailsTestConfig(server.URL))

	now := time.Now().UTC()
	args := ServicePerformanceDetailsArgs{
		ServiceName:  "svc",
		StartTimeISO: now.Add(-90 * 24 * time.Hour).Format(time.RFC3339),
		EndTimeISO:   now.Format(time.RFC3339),
		Env:          "prod",
	}

	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, args)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	// 90 days -> 3 chunks (35+35+20). 3 chunks * 6 range sub-queries, 3 * 3 instant sub-queries.
	if got := rangeCalls.Load(); got != 18 {
		t.Errorf("expected 18 range-query calls (3 chunks * 6 metrics), got %d", got)
	}
	if got := instantCalls.Load(); got != 9 {
		t.Errorf("expected 9 instant-query calls (3 chunks * 3 metrics), got %d", got)
	}
}

func TestServicePerformanceDetails_FailingChunkRecordsPartialErrorButOthersMerge(t *testing.T) {
	now := time.Now().UTC()
	start := now.Add(-40 * 24 * time.Hour).Unix() // 40 days -> 2 chunks (~20d each)
	end := now.Unix()
	wantChunks := splitIntoPerfDetailsChunks(start, end)
	if len(wantChunks) != 2 {
		t.Fatalf("expected 2 chunks for a 40 day window, got %d", len(wantChunks))
	}
	failChunkEnd := wantChunks[0].end

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload struct {
			Query     string `json:"query"`
			Timestamp int64  `json:"timestamp"`
			Window    int64  `json:"window"`
		}
		_ = json.Unmarshal(body, &payload)

		isApdex := strings.Contains(payload.Query, "trace_service_apdex_score")
		// Fail only the apdex sub-query for the first chunk.
		if r.URL.Path == constants.EndpointPromQuery && isApdex && payload.Timestamp == failChunkEnd {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error":"boom"}`))
			return
		}

		w.WriteHeader(http.StatusOK)
		if r.URL.Path == constants.EndpointPromQuery {
			resp := []map[string]any{
				{
					"metric": map[string]string{"quantile": "p95"},
					"values": [][]any{{payload.Timestamp, "42"}},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		w.Write([]byte("[]"))
	}))
	defer server.Close()

	handler := NewServicePerformanceDetailsHandler(server.Client(), perfDetailsTestConfig(server.URL))

	args := ServicePerformanceDetailsArgs{
		ServiceName:  "svc",
		StartTimeISO: time.Unix(start, 0).UTC().Format(time.RFC3339),
		EndTimeISO:   time.Unix(end, 0).UTC().Format(time.RFC3339),
		Env:          "prod",
	}

	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, args)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	text := result.Content[0].(*mcp.TextContent).Text
	var details ServicePerformanceDetails
	if err := json.Unmarshal([]byte(text), &details); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if len(details.PartialErrors) == 0 {
		t.Fatal("expected a partial error for the failing first chunk")
	}
	foundChunkBounds := false
	for _, e := range details.PartialErrors {
		if strings.HasPrefix(e, "chunk ") {
			foundChunkBounds = true
		}
	}
	if !foundChunkBounds {
		t.Errorf("expected a partial error message with chunk time bounds, got %+v", details.PartialErrors)
	}

	if len(details.ApdexScore) == 0 {
		t.Fatal("expected the second (successful) chunk's apdex data to still be present despite the first chunk failing")
	}
}

// --- gap 1: failing-chunk-still-merges-others for OTHER sub-queries ---

func TestServicePerformanceDetails_FailingChunkStillMergesOthers_Throughput(t *testing.T) {
	now := time.Now().UTC()
	start := now.Add(-40 * 24 * time.Hour).Unix() // 40 days -> 2 chunks (35d + 5d)
	end := now.Unix()
	wantChunks := splitIntoPerfDetailsChunks(start, end)
	if len(wantChunks) != 2 {
		t.Fatalf("expected 2 chunks for a 40 day window, got %d", len(wantChunks))
	}
	failChunkEnd := wantChunks[0].end

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload struct {
			Query     string `json:"query"`
			Timestamp int64  `json:"timestamp"`
		}
		_ = json.Unmarshal(body, &payload)

		isThroughput := strings.Contains(payload.Query, "sum by (http_status_code)(rate(")
		if r.URL.Path == constants.EndpointPromQuery && isThroughput && payload.Timestamp == failChunkEnd {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error":"boom"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		if r.URL.Path == constants.EndpointPromQuery && isThroughput {
			resp := []map[string]any{
				{"metric": map[string]string{"http_status_code": "200"}, "values": [][]any{{float64(payload.Timestamp), "5"}}},
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		w.Write([]byte("[]"))
	}))
	defer server.Close()

	handler := NewServicePerformanceDetailsHandler(server.Client(), perfDetailsTestConfig(server.URL))

	args := ServicePerformanceDetailsArgs{
		ServiceName:  "svc",
		StartTimeISO: time.Unix(start, 0).UTC().Format(time.RFC3339),
		EndTimeISO:   time.Unix(end, 0).UTC().Format(time.RFC3339),
		Env:          "prod",
	}

	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, args)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	text := result.Content[0].(*mcp.TextContent).Text
	var details ServicePerformanceDetails
	if err := json.Unmarshal([]byte(text), &details); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	foundThroughputPartialError := false
	for _, e := range details.PartialErrors {
		if strings.HasPrefix(e, "chunk ") && strings.Contains(e, "throughput") {
			foundThroughputPartialError = true
		}
	}
	if !foundThroughputPartialError {
		t.Errorf("expected a partial error for the failing throughput chunk, got %+v", details.PartialErrors)
	}
	if len(details.Throughput) == 0 {
		t.Fatal("expected the second (successful) chunk's throughput data to still merge despite the first chunk failing")
	}
}

func TestServicePerformanceDetails_FailingChunkStillMergesOthers_TopRTQuery(t *testing.T) {
	now := time.Now().UTC()
	start := now.Add(-40 * 24 * time.Hour).Unix() // 40 days -> 2 chunks (35d + 5d)
	end := now.Unix()
	wantChunks := splitIntoPerfDetailsChunks(start, end)
	if len(wantChunks) != 2 {
		t.Fatalf("expected 2 chunks for a 40 day window, got %d", len(wantChunks))
	}
	failChunkEnd := wantChunks[0].end

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload struct {
			Query     string `json:"query"`
			Timestamp int64  `json:"timestamp"`
		}
		_ = json.Unmarshal(body, &payload)

		isTopRT := strings.Contains(payload.Query, "trace_endpoint_duration")
		if r.URL.Path == constants.EndpointPromQueryInstant && isTopRT && payload.Timestamp == failChunkEnd {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error":"boom"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		if r.URL.Path == constants.EndpointPromQueryInstant && isTopRT {
			resp := []map[string]any{
				{"metric": map[string]string{"span_name": "op-x"}, "value": []any{float64(payload.Timestamp), "77"}},
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		w.Write([]byte("[]"))
	}))
	defer server.Close()

	handler := NewServicePerformanceDetailsHandler(server.Client(), perfDetailsTestConfig(server.URL))

	args := ServicePerformanceDetailsArgs{
		ServiceName:  "svc",
		StartTimeISO: time.Unix(start, 0).UTC().Format(time.RFC3339),
		EndTimeISO:   time.Unix(end, 0).UTC().Format(time.RFC3339),
		Env:          "prod",
	}

	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, args)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	text := result.Content[0].(*mcp.TextContent).Text
	var details ServicePerformanceDetails
	if err := json.Unmarshal([]byte(text), &details); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	foundPartialError := false
	for _, e := range details.PartialErrors {
		if strings.HasPrefix(e, "chunk ") && strings.Contains(e, "top_operations_by_response_time") {
			foundPartialError = true
		}
	}
	if !foundPartialError {
		t.Errorf("expected a partial error for the failing topRTQuery chunk, got %+v", details.PartialErrors)
	}

	if len(details.TopOperations.ByResponseTime) != 1 {
		t.Fatalf("expected the second (successful) chunk's top-response-time data to still merge, got %+v", details.TopOperations.ByResponseTime)
	}
	found := false
	for _, m := range details.TopOperations.ByResponseTime {
		for k, v := range m {
			if strings.HasPrefix(k, "op-x") && v == 77 {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("expected merged ByResponseTime to contain the surviving chunk's op-x=77 entry, got %+v", details.TopOperations.ByResponseTime)
	}
}

// --- gap 2: exact boundary values for both caps ---

func TestSplitIntoPerfDetailsChunks_Exactly35Days_NoSplit(t *testing.T) {
	start := int64(0)
	end := int64(perfDetailsMaxChunkDays) * 24 * 3600
	chunks := splitIntoPerfDetailsChunks(start, end)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk at exactly %d days, got %d: %+v", perfDetailsMaxChunkDays, len(chunks), chunks)
	}
}

func TestSplitIntoPerfDetailsChunks_35DaysPlus1Second_Splits(t *testing.T) {
	start := int64(0)
	end := int64(perfDetailsMaxChunkDays)*24*3600 + 1
	chunks := splitIntoPerfDetailsChunks(start, end)
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks at %d days + 1 second, got %d: %+v", perfDetailsMaxChunkDays, len(chunks), chunks)
	}
}

func TestServicePerformanceDetails_Exactly366Days_Accepted(t *testing.T) {
	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("[]"))
	}))
	defer server.Close()

	handler := NewServicePerformanceDetailsHandler(server.Client(), perfDetailsTestConfig(server.URL))

	now := time.Now().UTC()
	args := ServicePerformanceDetailsArgs{
		ServiceName:  "svc",
		StartTimeISO: now.Add(-time.Duration(maxServicePerformanceWindowDays) * 24 * time.Hour).Format(time.RFC3339),
		EndTimeISO:   now.Format(time.RFC3339),
		Env:          "prod",
	}

	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, args)
	if err != nil {
		t.Fatalf("expected exactly %d days to be accepted, got error: %v", maxServicePerformanceWindowDays, err)
	}
	if requestCount.Load() == 0 {
		t.Error("expected HTTP calls to be made for an accepted window")
	}
}

func TestServicePerformanceDetails_366DaysPlus1Second_Rejected(t *testing.T) {
	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("[]"))
	}))
	defer server.Close()

	handler := NewServicePerformanceDetailsHandler(server.Client(), perfDetailsTestConfig(server.URL))

	now := time.Now().UTC()
	args := ServicePerformanceDetailsArgs{
		ServiceName:  "svc",
		StartTimeISO: now.Add(-(time.Duration(maxServicePerformanceWindowDays)*24*time.Hour + time.Second)).Format(time.RFC3339),
		EndTimeISO:   now.Format(time.RFC3339),
		Env:          "prod",
	}

	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, args)
	if err == nil {
		t.Fatal("expected 366 days + 1 second to be rejected")
	}
	if !strings.Contains(err.Error(), "time range too wide") {
		t.Errorf("unexpected error message: %v", err)
	}
	// The message must not be self-contradictory: at exactly 366 days + 1
	// second, integer-dividing the rejected window's seconds into days would
	// report "got 366 days, max is 366 days" with no visible reason for the
	// rejection. Ceiling-dividing must report a "got" day count strictly
	// greater than the max.
	if !strings.Contains(err.Error(), fmt.Sprintf("got %d days", maxServicePerformanceWindowDays+1)) {
		t.Errorf("expected the message to report %d days (ceiling of 366 days + 1 second), got: %v", maxServicePerformanceWindowDays+1, err)
	}
	if strings.Contains(err.Error(), fmt.Sprintf("got %d days", maxServicePerformanceWindowDays)) {
		t.Errorf("message is self-contradictory: reports got=max (%d days) at the rejection boundary: %v", maxServicePerformanceWindowDays, err)
	}
	if rc := requestCount.Load(); rc != 0 {
		t.Errorf("expected no HTTP calls for a rejected window, got %d", rc)
	}
}

// --- gap 3: PromQL query-string construction correctness ---

func TestServicePerformanceDetails_QueryStrings_SingleChunkUses5mInnerSelector(t *testing.T) {
	var mu sync.Mutex
	var queries []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == constants.EndpointPromQuery {
			body, _ := io.ReadAll(r.Body)
			var payload struct {
				Query string `json:"query"`
			}
			_ = json.Unmarshal(body, &payload)
			mu.Lock()
			queries = append(queries, payload.Query)
			mu.Unlock()
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("[]"))
	}))
	defer server.Close()

	handler := NewServicePerformanceDetailsHandler(server.Client(), perfDetailsTestConfig(server.URL))

	now := time.Now().UTC()
	args := ServicePerformanceDetailsArgs{
		ServiceName:  "svc",
		StartTimeISO: now.Add(-10 * 24 * time.Hour).Format(time.RFC3339), // 10 days = 240h outer window
		EndTimeISO:   now.Format(time.RFC3339),
		Env:          "prod",
	}

	if _, _, err := handler(context.Background(), &mcp.CallToolRequest{}, args); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	throughputQueries := 0
	for _, q := range queries {
		if !strings.Contains(q, "sum by (http_status_code)(rate(") {
			continue
		}
		throughputQueries++
		if !strings.Contains(q, "[5m]") {
			t.Errorf("expected throughput query to use the fixed 5m inner selector, got: %s", q)
		}
		if strings.Contains(q, "[240h]") {
			t.Errorf("throughput query still uses the full outer window as inner selector: %s", q)
		}
	}
	if throughputQueries != 1 {
		t.Fatalf("expected exactly 1 throughput query for a single-chunk window, got %d", throughputQueries)
	}
}

func TestServicePerformanceDetails_QueryStrings_ChunkedUsesFixed5mRangeAndChunkWidthTopK(t *testing.T) {
	now := time.Now().UTC()
	start := now.Add(-90 * 24 * time.Hour).Unix() // 90 days -> 3 chunks (35+35+20)
	end := now.Unix()
	wantChunks := splitIntoPerfDetailsChunks(start, end)
	if len(wantChunks) != 3 {
		t.Fatalf("expected 3 chunks for a 90 day window, got %d", len(wantChunks))
	}
	chunkWidthMinutes := map[int64]int64{}
	for _, c := range wantChunks {
		chunkWidthMinutes[c.end] = (c.end - c.start) / 60
	}

	var mu sync.Mutex
	var rangeQueries []string
	var topRTEntries []string // "<timestamp>|<query>"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload struct {
			Query     string `json:"query"`
			Timestamp int64  `json:"timestamp"`
		}
		_ = json.Unmarshal(body, &payload)

		mu.Lock()
		switch {
		case r.URL.Path == constants.EndpointPromQuery && strings.Contains(payload.Query, "sum by (http_status_code)(rate("):
			rangeQueries = append(rangeQueries, payload.Query)
		case r.URL.Path == constants.EndpointPromQueryInstant && strings.Contains(payload.Query, "trace_endpoint_duration"):
			topRTEntries = append(topRTEntries, fmt.Sprintf("%d|%s", payload.Timestamp, payload.Query))
		}
		mu.Unlock()

		w.WriteHeader(http.StatusOK)
		w.Write([]byte("[]"))
	}))
	defer server.Close()

	handler := NewServicePerformanceDetailsHandler(server.Client(), perfDetailsTestConfig(server.URL))

	args := ServicePerformanceDetailsArgs{
		ServiceName:  "svc",
		StartTimeISO: time.Unix(start, 0).UTC().Format(time.RFC3339),
		EndTimeISO:   time.Unix(end, 0).UTC().Format(time.RFC3339),
		Env:          "prod",
	}
	if _, _, err := handler(context.Background(), &mcp.CallToolRequest{}, args); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if len(rangeQueries) != 3 {
		t.Fatalf("expected 3 throughput range queries (one per chunk), got %d", len(rangeQueries))
	}
	for _, q := range rangeQueries {
		if !strings.Contains(q, "[5m]") {
			t.Errorf("expected chunked range query to still use the fixed 5m inner selector, got: %s", q)
		}
	}

	if len(topRTEntries) != 3 {
		t.Fatalf("expected 3 topRTQuery calls (one per chunk), got %d", len(topRTEntries))
	}
	for _, entry := range topRTEntries {
		parts := strings.SplitN(entry, "|", 2)
		ts, _ := strconv.ParseInt(parts[0], 10, 64)
		q := parts[1]
		wantWidth, ok := chunkWidthMinutes[ts]
		if !ok {
			t.Fatalf("topRTQuery timestamp %d doesn't match any expected chunk end", ts)
		}
		wantSelector := fmt.Sprintf("[%dm]", wantWidth)
		if !strings.Contains(q, wantSelector) {
			t.Errorf("expected topRTQuery for chunk ending %d to use its own width %s as inner selector, got: %s", ts, wantSelector, q)
		}
		if strings.Contains(q, "[5m]") {
			t.Errorf("topRTQuery should use the chunk's own width, not the fixed 5m selector: %s", q)
		}
		if !strings.Contains(q, "topk(20,") {
			t.Errorf("expected over-fetch topk(20,...) for a multi-chunk window, got: %s", q)
		}
		if strings.Contains(q, "topk(10,") {
			t.Errorf("expected multi-chunk window to NOT use topk(10,...), got: %s", q)
		}
	}
}

// --- gap 4: multi-chunk top-k merge at the handler level ---

func TestServicePerformanceDetails_MultiChunkTopKMergeAtHandlerLevel(t *testing.T) {
	now := time.Now().UTC()
	start := now.Add(-90 * 24 * time.Hour).Unix() // 90 days -> 3 chunks (35+35+20)
	end := now.Unix()
	wantChunks := splitIntoPerfDetailsChunks(start, end)
	if len(wantChunks) != 3 {
		t.Fatalf("expected 3 chunks for a 90 day window, got %d", len(wantChunks))
	}

	// topRTQuery and topErrQuery keys are built from span_name (+ other,
	// here-empty, label fields); topErrorsQuery keys are the exception_type
	// value directly. See NewServicePerformanceDetailsHandler.
	rtByChunkEnd := map[int64]map[string]string{
		wantChunks[0].end: {"op-a": "100"},
		wantChunks[1].end: {"op-a": "50", "op-b": "200"},
		wantChunks[2].end: {"op-c": "80"},
	}
	errByChunkEnd := map[int64]map[string]string{
		wantChunks[0].end: {"op-err1": "5"},
		wantChunks[1].end: {"op-err1": "3"},
		wantChunks[2].end: {"op-err2": "10"},
	}
	topErrorsByChunkEnd := map[int64]map[string]string{
		wantChunks[0].end: {"500": "5"},
		wantChunks[1].end: {"500": "3"},
		wantChunks[2].end: {"404": "20"},
	}

	writeInstant := func(w http.ResponseWriter, keyField string, m map[string]string) {
		items := make([]map[string]any, 0, len(m))
		for k, v := range m {
			items = append(items, map[string]any{
				"metric": map[string]string{keyField: k},
				"value":  []any{float64(0), v},
			})
		}
		_ = json.NewEncoder(w).Encode(items)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload struct {
			Query     string `json:"query"`
			Timestamp int64  `json:"timestamp"`
		}
		_ = json.Unmarshal(body, &payload)

		w.WriteHeader(http.StatusOK)
		if r.URL.Path != constants.EndpointPromQueryInstant {
			w.Write([]byte("[]"))
			return
		}

		switch {
		case strings.Contains(payload.Query, "trace_endpoint_duration"):
			writeInstant(w, "span_name", rtByChunkEnd[payload.Timestamp])
		case strings.Contains(payload.Query, "process_runtime_name, exception_type)"):
			writeInstant(w, "span_name", errByChunkEnd[payload.Timestamp])
		case strings.Contains(payload.Query, "sum by (exception_type)(sum by (exception_type, span_kind)"):
			writeInstant(w, "exception_type", topErrorsByChunkEnd[payload.Timestamp])
		default:
			w.Write([]byte("[]"))
		}
	}))
	defer server.Close()

	handler := NewServicePerformanceDetailsHandler(server.Client(), perfDetailsTestConfig(server.URL))

	args := ServicePerformanceDetailsArgs{
		ServiceName:  "svc",
		StartTimeISO: time.Unix(start, 0).UTC().Format(time.RFC3339),
		EndTimeISO:   time.Unix(end, 0).UTC().Format(time.RFC3339),
		Env:          "prod",
	}

	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, args)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	text := result.Content[0].(*mcp.TextContent).Text
	var details ServicePerformanceDetails
	if err := json.Unmarshal([]byte(text), &details); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	rtKey := func(spanName string) string {
		return fmt.Sprintf("%s-%s-%s-%s-%s-%s-%s", spanName, "", "", "", "", "", "")
	}

	wantRT := []map[string]float64{{rtKey("op-b"): 200}, {rtKey("op-a"): 100}, {rtKey("op-c"): 80}}
	if !reflect.DeepEqual(details.TopOperations.ByResponseTime, wantRT) {
		t.Errorf("expected cross-chunk max-merged ByResponseTime %+v, got %+v", wantRT, details.TopOperations.ByResponseTime)
	}

	wantErrRate := []map[string]int64{{rtKey("op-err2"): 10}, {rtKey("op-err1"): 8}}
	if !reflect.DeepEqual(details.TopOperations.ByErrorRate, wantErrRate) {
		t.Errorf("expected cross-chunk summed ByErrorRate %+v, got %+v", wantErrRate, details.TopOperations.ByErrorRate)
	}

	wantTopErrors := []map[string]int64{{"404": 20}, {"500": 8}}
	if !reflect.DeepEqual(details.TopErrors, wantTopErrors) {
		t.Errorf("expected cross-chunk summed TopErrors %+v, got %+v", wantTopErrors, details.TopErrors)
	}
}

// --- gap 5: >2 chunks end-to-end, contiguous deduped merge across all boundaries ---

func TestServicePerformanceDetails_ThreeChunkWindow_MergedSeriesOrderedContiguousDeduped(t *testing.T) {
	now := time.Now().UTC()
	start := now.Add(-100 * 24 * time.Hour).Unix() // 100 days -> 3 chunks (35+35+30)
	end := now.Unix()
	wantChunks := splitIntoPerfDetailsChunks(start, end)
	if len(wantChunks) != 3 {
		t.Fatalf("expected 3 chunks for a 100 day window (35+35+30), got %d: %+v", len(wantChunks), wantChunks)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload struct {
			Query     string `json:"query"`
			Timestamp int64  `json:"timestamp"`
			Window    int64  `json:"window"`
		}
		_ = json.Unmarshal(body, &payload)

		w.WriteHeader(http.StatusOK)
		if r.URL.Path == constants.EndpointPromQuery && strings.Contains(payload.Query, "trace_service_apdex_score") {
			chunkStart := payload.Timestamp - payload.Window
			resp := []map[string]any{
				{
					"metric": map[string]string{},
					"values": [][]any{
						{float64(chunkStart), "1"},
						{float64(payload.Timestamp), "1"},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		w.Write([]byte("[]"))
	}))
	defer server.Close()

	handler := NewServicePerformanceDetailsHandler(server.Client(), perfDetailsTestConfig(server.URL))

	args := ServicePerformanceDetailsArgs{
		ServiceName:  "svc",
		StartTimeISO: time.Unix(start, 0).UTC().Format(time.RFC3339),
		EndTimeISO:   time.Unix(end, 0).UTC().Format(time.RFC3339),
		Env:          "prod",
	}

	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, args)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	text := result.Content[0].(*mcp.TextContent).Text
	var details ServicePerformanceDetails
	if err := json.Unmarshal([]byte(text), &details); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if len(details.ApdexScore) != 1 {
		t.Fatalf("expected a single merged apdex series (one label set across all chunks), got %d: %+v", len(details.ApdexScore), details.ApdexScore)
	}

	wantTimestamps := []uint64{
		uint64(wantChunks[0].start),
		uint64(wantChunks[0].end), // == wantChunks[1].start, deduped
		uint64(wantChunks[1].end), // == wantChunks[2].start, deduped
		uint64(wantChunks[2].end),
	}
	got := details.ApdexScore[0].Values
	if len(got) != len(wantTimestamps) {
		t.Fatalf("expected %d merged points across the 3 chunk boundaries, got %d: %+v", len(wantTimestamps), len(got), got)
	}
	for i, wantTs := range wantTimestamps {
		if got[i].Timestamp != wantTs {
			t.Errorf("point %d: expected timestamp %d, got %d (full: %+v)", i, wantTs, got[i].Timestamp, got)
		}
	}
	for i := 1; i < len(got); i++ {
		if got[i].Timestamp <= got[i-1].Timestamp {
			t.Errorf("expected strictly increasing/contiguous timestamps, got %+v", got)
		}
	}
}

// --- gap 6: malformed (unparseable) JSON body for a range-vector chunk ---

func TestServicePerformanceDetails_MalformedJSONChunk_RecordsPartialErrorOthersMerge(t *testing.T) {
	now := time.Now().UTC()
	start := now.Add(-40 * 24 * time.Hour).Unix() // 40 days -> 2 chunks (35d + 5d)
	end := now.Unix()
	wantChunks := splitIntoPerfDetailsChunks(start, end)
	if len(wantChunks) != 2 {
		t.Fatalf("expected 2 chunks for a 40 day window, got %d", len(wantChunks))
	}
	firstChunkEnd := wantChunks[0].end

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload struct {
			Query     string `json:"query"`
			Timestamp int64  `json:"timestamp"`
		}
		_ = json.Unmarshal(body, &payload)

		isErrorRate := strings.Contains(payload.Query, "sum by (service_name, http_status_code)(rate(")
		w.WriteHeader(http.StatusOK)
		if r.URL.Path == constants.EndpointPromQuery && isErrorRate {
			if payload.Timestamp == firstChunkEnd {
				// 200 OK but a garbage body - must be recorded as a partial
				// parse error, not abort the whole call.
				w.Write([]byte("not valid json"))
				return
			}
			resp := []map[string]any{
				{"metric": map[string]string{"http_status_code": "500"}, "values": [][]any{{float64(payload.Timestamp), "3"}}},
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		w.Write([]byte("[]"))
	}))
	defer server.Close()

	handler := NewServicePerformanceDetailsHandler(server.Client(), perfDetailsTestConfig(server.URL))

	args := ServicePerformanceDetailsArgs{
		ServiceName:  "svc",
		StartTimeISO: time.Unix(start, 0).UTC().Format(time.RFC3339),
		EndTimeISO:   time.Unix(end, 0).UTC().Format(time.RFC3339),
		Env:          "prod",
	}

	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, args)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	text := result.Content[0].(*mcp.TextContent).Text
	var details ServicePerformanceDetails
	if err := json.Unmarshal([]byte(text), &details); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	foundParseError := false
	for _, e := range details.PartialErrors {
		if strings.HasPrefix(e, "chunk ") && strings.Contains(e, "failed to parse") {
			foundParseError = true
		}
	}
	if !foundParseError {
		t.Errorf("expected a partial error for the malformed-JSON chunk, got %+v", details.PartialErrors)
	}

	if len(details.ErrorRate) == 0 {
		t.Fatal("expected the second (valid) chunk's error_rate data to still merge despite the first chunk returning malformed JSON")
	}
}

// --- top_n arg ---

// TestServicePerformanceDetails_TopN covers the new optional top_n arg
// (default 10) applied uniformly to top_operations_by_response_time,
// top_operations_by_error_rate, and top_errors, for both a single-chunk
// (<=35 day) and a multi-chunk (>35 day) window.
func TestServicePerformanceDetails_TopN(t *testing.T) {
	makeRows := func(keyField string, n int) []map[string]any {
		rows := make([]map[string]any, 0, n)
		for i := 0; i < n; i++ {
			rows = append(rows, map[string]any{
				"metric": map[string]string{keyField: fmt.Sprintf("%s-%02d", keyField, i)},
				"value":  []any{float64(0), fmt.Sprintf("%d", 1000-i)}, // distinct, descending values
			})
		}
		return rows
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload struct {
			Query string `json:"query"`
		}
		_ = json.Unmarshal(body, &payload)
		w.WriteHeader(http.StatusOK)
		switch {
		case r.URL.Path != constants.EndpointPromQueryInstant:
			w.Write([]byte("[]"))
		case strings.Contains(payload.Query, "trace_endpoint_duration"):
			_ = json.NewEncoder(w).Encode(makeRows("span_name", 30))
		case strings.Contains(payload.Query, "process_runtime_name, exception_type)"):
			_ = json.NewEncoder(w).Encode(makeRows("span_name", 30))
		case strings.Contains(payload.Query, "sum by (exception_type)(sum by (exception_type, span_kind)"):
			_ = json.NewEncoder(w).Encode(makeRows("exception_type", 30))
		default:
			w.Write([]byte("[]"))
		}
	}))
	defer server.Close()

	handler := NewServicePerformanceDetailsHandler(server.Client(), perfDetailsTestConfig(server.URL))

	for _, windowCase := range []struct {
		name       string
		windowDays int
	}{
		{"single_chunk", 10},
		{"multi_chunk", 90},
	} {
		t.Run(windowCase.name, func(t *testing.T) {
			for _, tc := range []struct {
				name  string
				topN  int
				wantN int
			}{
				{"default_unset", 0, perfDetailsDefaultTopN},
				{"n3", 3, 3},
				{"n25", 25, 25},
			} {
				t.Run(tc.name, func(t *testing.T) {
					now := time.Now().UTC()
					args := ServicePerformanceDetailsArgs{
						ServiceName:  "svc",
						StartTimeISO: now.Add(-time.Duration(windowCase.windowDays) * 24 * time.Hour).Format(time.RFC3339),
						EndTimeISO:   now.Format(time.RFC3339),
						Env:          "prod",
						TopN:         tc.topN,
					}
					result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, args)
					if err != nil {
						t.Fatalf("handler returned error: %v", err)
					}
					text := result.Content[0].(*mcp.TextContent).Text
					var details ServicePerformanceDetails
					if err := json.Unmarshal([]byte(text), &details); err != nil {
						t.Fatalf("failed to unmarshal response: %v", err)
					}
					if len(details.TopOperations.ByResponseTime) != tc.wantN {
						t.Errorf("ByResponseTime: expected %d entries, got %d: %+v", tc.wantN, len(details.TopOperations.ByResponseTime), details.TopOperations.ByResponseTime)
					}
					if len(details.TopOperations.ByErrorRate) != tc.wantN {
						t.Errorf("ByErrorRate: expected %d entries, got %d: %+v", tc.wantN, len(details.TopOperations.ByErrorRate), details.TopOperations.ByErrorRate)
					}
					if len(details.TopErrors) != tc.wantN {
						t.Errorf("TopErrors: expected %d entries, got %d: %+v", tc.wantN, len(details.TopErrors), details.TopErrors)
					}
				})
			}
		})
	}
}

// TestServicePerformanceDetails_TopNClampsToMax asserts a huge requested
// top_n is clamped to perfDetailsMaxTopN rather than being sent to the
// backend unbounded, and that the multi-chunk over-fetch ratio (2*topN) is
// computed from the clamped value, not the raw input.
func TestServicePerformanceDetails_TopNClampsToMax(t *testing.T) {
	var mu sync.Mutex
	var topRTQueries []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == constants.EndpointPromQueryInstant {
			body, _ := io.ReadAll(r.Body)
			var payload struct {
				Query string `json:"query"`
			}
			_ = json.Unmarshal(body, &payload)
			if strings.Contains(payload.Query, "trace_endpoint_duration") {
				mu.Lock()
				topRTQueries = append(topRTQueries, payload.Query)
				mu.Unlock()
			}
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("[]"))
	}))
	defer server.Close()

	handler := NewServicePerformanceDetailsHandler(server.Client(), perfDetailsTestConfig(server.URL))

	now := time.Now().UTC()
	args := ServicePerformanceDetailsArgs{
		ServiceName:  "svc",
		StartTimeISO: now.Add(-90 * 24 * time.Hour).Format(time.RFC3339), // 90 days -> multi-chunk
		EndTimeISO:   now.Format(time.RFC3339),
		Env:          "prod",
		TopN:         1000000,
	}

	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, args)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	text := result.Content[0].(*mcp.TextContent).Text
	var details ServicePerformanceDetails
	if err := json.Unmarshal([]byte(text), &details); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if len(details.TopOperations.ByResponseTime) > perfDetailsMaxTopN {
		t.Errorf("expected ByResponseTime capped at %d entries, got %d", perfDetailsMaxTopN, len(details.TopOperations.ByResponseTime))
	}

	wantOverFetch := fmt.Sprintf("topk(%d,", 2*perfDetailsMaxTopN)
	unwantedOverFetch := "topk(2000000,"
	if len(topRTQueries) == 0 {
		t.Fatal("expected at least one topRTQuery call")
	}
	for _, q := range topRTQueries {
		if strings.Contains(q, unwantedOverFetch) {
			t.Errorf("expected the multi-chunk over-fetch to be computed from the clamped top_n, got unclamped query: %s", q)
		}
		if !strings.Contains(q, wantOverFetch) {
			t.Errorf("expected topRTQuery to use the clamped over-fetch %s, got: %s", wantOverFetch, q)
		}
	}
}

// --- per-chunk HTTP timeout applies only on the genuinely-chunked path ---

// deadlineRecordingTransport wraps a RoundTripper and records, for every
// outgoing request, whether req.Context() carried a deadline. The context
// on outgoing requests is the client-side chunkCtx built by
// fetchChunkedRangeSeries/fetchChunkedTopK (via
// utils.MakePromRangeAPIQuery/MakePromInstantAPIQuery's
// http.NewRequestWithContext) — this is where constants.PerChunkHTTPTimeout
// actually lives. The inbound context on the *server* side
// (httptest handler's r.Context()) is a different context tied to the TCP
// connection/server lifecycle; it never carries the client's deadline, so
// asserting there would not test anything.
type deadlineRecordingTransport struct {
	base http.RoundTripper

	mu            sync.Mutex
	sawDeadline   bool
	sawNoDeadline bool
}

func (t *deadlineRecordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	_, hasDeadline := req.Context().Deadline()
	t.mu.Lock()
	if hasDeadline {
		t.sawDeadline = true
	} else {
		t.sawNoDeadline = true
	}
	t.mu.Unlock()
	return t.base.RoundTrip(req)
}

// TestServicePerformanceDetails_PerChunkTimeoutOnlyAppliedWhenChunked asserts
// the contract structurally, at the client's own outgoing request context: a
// single-chunk (<=35 day) call must run under the caller's own context with
// no added bound (no deadline), while a multi-chunk (>35 day) call must have
// the constants.PerChunkHTTPTimeout bound applied per chunk (a deadline is
// present). This is deterministic and doesn't depend on sleeps or
// wall-clock timing.
func TestServicePerformanceDetails_PerChunkTimeoutOnlyAppliedWhenChunked(t *testing.T) {
	// 1h single-chunk window: every outgoing sub-query request must carry no
	// deadline.
	t.Run("1h_single_chunk_no_deadline", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("[]"))
		}))
		defer server.Close()

		transport := &deadlineRecordingTransport{base: http.DefaultTransport}
		client := &http.Client{Transport: transport}

		handler := NewServicePerformanceDetailsHandler(client, perfDetailsTestConfig(server.URL))
		now := time.Now().UTC()
		args := ServicePerformanceDetailsArgs{
			ServiceName:  "svc",
			Env:          "prod",
			StartTimeISO: now.Add(-1 * time.Hour).Format(time.RFC3339),
			EndTimeISO:   now.Format(time.RFC3339),
		}
		_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, args)
		if err != nil {
			t.Fatalf("handler returned error: %v", err)
		}
		if !transport.sawNoDeadline {
			t.Error("expected at least one sub-query request with no deadline on the single-chunk path")
		}
		if transport.sawDeadline {
			t.Error("expected no sub-query request to carry a deadline on the single-chunk path")
		}
	})

	// 90 day multi-chunk window (3 chunks): every outgoing sub-query request
	// must carry a deadline (the constants.PerChunkHTTPTimeout bound).
	t.Run("90d_multi_chunk_has_deadline", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("[]"))
		}))
		defer server.Close()

		transport := &deadlineRecordingTransport{base: http.DefaultTransport}
		client := &http.Client{Transport: transport}

		handler := NewServicePerformanceDetailsHandler(client, perfDetailsTestConfig(server.URL))
		now := time.Now().UTC()
		args := ServicePerformanceDetailsArgs{
			ServiceName:  "svc",
			Env:          "prod",
			StartTimeISO: now.Add(-90 * 24 * time.Hour).Format(time.RFC3339),
			EndTimeISO:   now.Format(time.RFC3339),
		}
		_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, args)
		if err != nil {
			t.Fatalf("handler returned error: %v", err)
		}
		if !transport.sawDeadline {
			t.Error("expected at least one sub-query request with a deadline on the multi-chunk path")
		}
		if transport.sawNoDeadline {
			t.Error("expected no sub-query request to run without a deadline on the multi-chunk path")
		}
	})
}
