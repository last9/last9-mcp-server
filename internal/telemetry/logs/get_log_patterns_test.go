package logs

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type logSearchStub struct {
	mu     sync.Mutex
	bodies []map[string]any
	// envelope holds extra top-level fields (logs_truncated, search_stats)
	// added to every response.
	envelope map[string]any
}

// serve answers each log search with the lines listed for the request's
// start second, so a test can give a baseline and a current window different
// content.
func (s *logSearchStub) serve(t *testing.T, linesByStart map[int64][]string, totals map[int64]int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(s.handler(linesByStart, totals))
	t.Cleanup(srv.Close)
	return srv
}

func (s *logSearchStub) handler(linesByStart map[int64][]string, totals map[int64]int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		s.mu.Lock()
		s.bodies = append(s.bodies, body)
		s.mu.Unlock()

		start := int64(body["start"].(float64))
		values := make([]any, 0)
		for i, line := range linesByStart[start] {
			values = append(values, []any{fmt.Sprint((start + int64(i)) * 1e9), line})
		}
		resp := map[string]any{
			"query_result": map[string]any{"status": "success", "data": map[string]any{
				"resultType": "streams",
				"result":     []any{map[string]any{"stream": map[string]any{"severity": "ERROR"}, "values": values}},
			}},
		}
		if total, ok := totals[start]; ok {
			resp["total_matching_lines"] = total
		}
		for key, value := range s.envelope {
			resp[key] = value
		}
		_ = json.NewEncoder(w).Encode(resp)
	}
}

func (s *logSearchStub) requests() []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]map[string]any(nil), s.bodies...)
}

func repeatLines(line string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf(line, i)
	}
	return out
}

func decodeToolJSON[T any](t *testing.T, result *mcp.CallToolResult) T {
	t.Helper()
	var out T
	text := result.Content[0].(*mcp.TextContent).Text
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("response is not JSON: %v\n%s", err, text)
	}
	return out
}

const (
	patternWindowStartISO = "2026-10-16T10:00:00Z"
	patternWindowEndISO   = "2026-10-16T11:00:00Z"
	patternWindowStart    = int64(1792144800)
)

func TestGetLogPatterns_ClustersSampleAndFoldsTailIntoOther(t *testing.T) {
	lines := append(repeatLines("cache miss for key %d", 3), repeatLines("timeout calling payments after %d ms", 5)...)
	lines = append(lines, "shutting down worker", "worker pool resized")
	stub := &logSearchStub{}
	srv := stub.serve(t, map[int64][]string{patternWindowStart: lines}, map[int64]int{patternWindowStart: 870})

	result, _, err := NewGetLogPatternsHandler(srv.Client(), apiModeCfg(srv.URL))(context.Background(), &mcp.CallToolRequest{}, GetLogPatternsArgs{
		LogPatternScopeArgs: LogPatternScopeArgs{
			ServiceName: "checkout", Env: "prod", StartTimeISO: patternWindowStartISO, EndTimeISO: patternWindowEndISO, SampleSize: 10,
		},
		TopN: 2,
	})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	got := decodeToolJSON[GetLogPatternsResponse](t, result)

	if got.Service != "checkout" || got.Env != "prod" {
		t.Errorf("scope = %q/%q", got.Service, got.Env)
	}
	if got.Window.SampledLines != 10 || !got.Window.SampleLimitReached {
		t.Errorf("window = %+v, want 10 sampled lines with the limit reached", got.Window)
	}
	if got.Window.TotalMatchingLines == nil || *got.Window.TotalMatchingLines != 870 {
		t.Errorf("total_matching_lines = %v, want 870", got.Window.TotalMatchingLines)
	}
	if got.TotalPatterns != 4 || len(got.Patterns) != 2 {
		t.Fatalf("total_patterns = %d, patterns = %+v; want 4 total, top 2 returned", got.TotalPatterns, got.Patterns)
	}
	top := got.Patterns[0]
	if top.Template != "timeout calling payments after <*> ms" || top.Count != 5 || top.Sample != "timeout calling payments after 0 ms" {
		t.Errorf("top pattern = %+v", top)
	}
	if top.FirstSeen != "2026-10-16T10:00:03Z" || top.LastSeen != "2026-10-16T10:00:07Z" || top.SeverityCounts["ERROR"] != 5 {
		t.Errorf("top pattern stats = %+v", top.LogPatternStats)
	}
	if got.Patterns[1].Template != "cache miss for key <*>" || got.Patterns[1].Count != 3 {
		t.Errorf("second pattern = %+v", got.Patterns[1])
	}
	if got.Other == nil || got.Other.Patterns != 2 || got.Other.Count != 2 {
		t.Errorf("other = %+v, want the 2 remaining single-line patterns", got.Other)
	}

	reqs := stub.requests()
	if len(reqs) != 1 {
		t.Fatalf("requests = %d, want one search", len(reqs))
	}
	if reqs[0]["limit"] != float64(10) {
		t.Errorf("limit = %v, want sample_size", reqs[0]["limit"])
	}
	pipeline, _ := json.Marshal(reqs[0]["pipeline"])
	for _, want := range []string{`"ServiceName","checkout"`, `"resources['deployment.environment']","prod"`} {
		if !strings.Contains(string(pipeline), want) {
			t.Errorf("pipeline %s missing %s", pipeline, want)
		}
	}
}

func TestGetLogPatterns_EmptyWindowIsAValidAnswer(t *testing.T) {
	stub := &logSearchStub{}
	srv := stub.serve(t, nil, nil)

	result, _, err := NewGetLogPatternsHandler(srv.Client(), apiModeCfg(srv.URL))(context.Background(), &mcp.CallToolRequest{}, GetLogPatternsArgs{
		LogPatternScopeArgs: LogPatternScopeArgs{ServiceName: "checkout", StartTimeISO: patternWindowStartISO, EndTimeISO: patternWindowEndISO},
	})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, `"patterns": []`) {
		t.Errorf("empty result must serve patterns as [], got:\n%s", text)
	}
	got := decodeToolJSON[GetLogPatternsResponse](t, result)
	if got.TotalPatterns != 0 || got.Other != nil || got.Window.SampledLines != 0 || got.Window.SampleLimitReached {
		t.Errorf("response = %+v, want an empty, complete window", got)
	}
	if reqs := stub.requests(); reqs[0]["limit"] != float64(defaultPatternSampleSize) {
		t.Errorf("limit = %v, want default sample size %d", reqs[0]["limit"], defaultPatternSampleSize)
	}
}

func TestGetLogPatterns_RejectsInvalidArgsBeforeQuerying(t *testing.T) {
	cases := map[string]GetLogPatternsArgs{
		"missing service":       {},
		"blank service":         {LogPatternScopeArgs: LogPatternScopeArgs{ServiceName: "  "}},
		"sample over max":       {LogPatternScopeArgs: LogPatternScopeArgs{ServiceName: "a", SampleSize: maxPatternSampleSize + 1}},
		"negative sample":       {LogPatternScopeArgs: LogPatternScopeArgs{ServiceName: "a", SampleSize: -1}},
		"top_n over max":        {LogPatternScopeArgs: LogPatternScopeArgs{ServiceName: "a"}, TopN: maxPatternTopN + 1},
		"negative top_n":        {LogPatternScopeArgs: LogPatternScopeArgs{ServiceName: "a"}, TopN: -1},
		"negative lookback":     {LogPatternScopeArgs: LogPatternScopeArgs{ServiceName: "a", LookbackMinutes: -5}},
		"bad index":             {LogPatternScopeArgs: LogPatternScopeArgs{ServiceName: "a", Index: "nope"}},
		"start after end":       {LogPatternScopeArgs: LogPatternScopeArgs{ServiceName: "a", StartTimeISO: patternWindowEndISO, EndTimeISO: patternWindowStartISO}},
		"start equals end":      {LogPatternScopeArgs: LogPatternScopeArgs{ServiceName: "a", StartTimeISO: patternWindowStartISO, EndTimeISO: patternWindowStartISO}},
		"unparseable timestamp": {LogPatternScopeArgs: LogPatternScopeArgs{ServiceName: "a", StartTimeISO: "yesterday"}},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			stub := &logSearchStub{}
			srv := stub.serve(t, nil, nil)
			_, _, err := NewGetLogPatternsHandler(srv.Client(), apiModeCfg(srv.URL))(context.Background(), &mcp.CallToolRequest{}, args)
			if err == nil {
				t.Fatal("want an error")
			}
			if n := len(stub.requests()); n != 0 {
				t.Errorf("made %d requests before rejecting", n)
			}
		})
	}
}

func TestGetLogPatterns_AcceptsLimitsAtTheirMaximum(t *testing.T) {
	stub := &logSearchStub{}
	srv := stub.serve(t, nil, nil)
	_, _, err := NewGetLogPatternsHandler(srv.Client(), apiModeCfg(srv.URL))(context.Background(), &mcp.CallToolRequest{}, GetLogPatternsArgs{
		LogPatternScopeArgs: LogPatternScopeArgs{ServiceName: "a", SampleSize: maxPatternSampleSize},
		TopN:                maxPatternTopN,
	})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if reqs := stub.requests(); reqs[0]["limit"] != float64(maxPatternSampleSize) {
		t.Errorf("limit = %v, want %d", reqs[0]["limit"], maxPatternSampleSize)
	}
}

func TestGetLogPatterns_SampleLimitFollowsTheUpstreamTotal(t *testing.T) {
	cases := []struct {
		name      string
		lines     int
		total     *int
		truncated any
		want      bool
	}{
		{"exact fill with matching total", 4, intPtr(4), nil, false},
		{"exact fill with unknown total", 4, nil, nil, true},
		{"exact fill with a larger total", 4, intPtr(5), nil, true},
		{"short sample with a larger total", 3, intPtr(9), nil, true},
		{"short sample with unknown total", 3, nil, nil, false},
		{"matching total that is only a floor", 4, intPtr(4), true, true},
		{"empty sample of a truncated search", 0, intPtr(0), true, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			stub := &logSearchStub{}
			if c.truncated != nil {
				stub.envelope = map[string]any{"logs_truncated": c.truncated}
			}
			totals := map[int64]int{}
			if c.total != nil {
				totals[patternWindowStart] = *c.total
			}
			srv := stub.serve(t, map[int64][]string{patternWindowStart: sameLines("heartbeat ok", c.lines)}, totals)
			result, _, err := NewGetLogPatternsHandler(srv.Client(), apiModeCfg(srv.URL))(context.Background(), &mcp.CallToolRequest{}, GetLogPatternsArgs{
				LogPatternScopeArgs: LogPatternScopeArgs{ServiceName: "checkout", StartTimeISO: patternWindowStartISO, EndTimeISO: patternWindowEndISO, SampleSize: 4},
			})
			if err != nil {
				t.Fatalf("handler: %v", err)
			}
			got := decodeToolJSON[GetLogPatternsResponse](t, result)
			if got.Window.SampleLimitReached != c.want {
				t.Errorf("sample_limit_reached = %t, want %t (window %+v)", got.Window.SampleLimitReached, c.want, got.Window)
			}
			if c.truncated == true && (got.Window.LogsTruncated == nil || !*got.Window.LogsTruncated) {
				t.Errorf("logs_truncated = %v, want true carried into the window", got.Window.LogsTruncated)
			}
		})
	}
}

func TestGetLogPatterns_RejectsASearchWithFailedChunks(t *testing.T) {
	for _, failed := range []int{0, 1} {
		t.Run(fmt.Sprintf("chunks_failed=%d", failed), func(t *testing.T) {
			stub := &logSearchStub{envelope: map[string]any{"search_stats": map[string]any{"chunks_failed": failed}}}
			srv := stub.serve(t, nil, map[int64]int{patternWindowStart: 0})
			_, _, err := NewGetLogPatternsHandler(srv.Client(), apiModeCfg(srv.URL))(context.Background(), &mcp.CallToolRequest{}, GetLogPatternsArgs{
				LogPatternScopeArgs: LogPatternScopeArgs{ServiceName: "checkout", StartTimeISO: patternWindowStartISO, EndTimeISO: patternWindowEndISO},
			})
			if failed == 0 && err != nil {
				t.Fatalf("complete empty search rejected: %v", err)
			}
			if failed > 0 && (err == nil || !strings.Contains(err.Error(), "1 of its search chunks failed")) {
				t.Fatalf("err = %v, want the incomplete search rejected", err)
			}
		})
	}
}

func TestGetLogPatterns_RejectsAnUnreadableChunksFailed(t *testing.T) {
	stub := &logSearchStub{envelope: map[string]any{"search_stats": map[string]any{"chunks_failed": "1"}}}
	srv := stub.serve(t, nil, map[int64]int{patternWindowStart: 0})
	_, _, err := NewGetLogPatternsHandler(srv.Client(), apiModeCfg(srv.URL))(context.Background(), &mcp.CallToolRequest{}, GetLogPatternsArgs{
		LogPatternScopeArgs: LogPatternScopeArgs{ServiceName: "checkout", StartTimeISO: patternWindowStartISO, EndTimeISO: patternWindowEndISO},
	})
	if err == nil || !strings.Contains(err.Error(), "chunks_failed") {
		t.Fatalf("err = %v, want a search whose completeness cannot be read rejected", err)
	}
}

func intPtr(v int) *int { return &v }

// chunkedLogsServer answers the pre-search-API chunked path, which reports no
// totals or search_stats, so completeness rests on the sample size alone.
func chunkedLogsServer(t *testing.T, status int, lines ...string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		hits.Add(1)
		if status != http.StatusOK {
			http.Error(w, "chunk failed", status)
			return
		}
		values := make([]any, 0, len(lines))
		for i, line := range lines {
			values = append(values, []any{fmt.Sprint((patternWindowStart + int64(i)) * 1e9), line})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": map[string]any{
			"resultType": "streams",
			"result":     []any{map[string]any{"stream": map[string]any{"severity": "ERROR"}, "values": values}},
		}})
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

func callLogPatternsChunked(srv *httptest.Server, sampleSize int) (*mcp.CallToolResult, error) {
	cfg := apiModeCfg(srv.URL)
	cfg.UseLogSearchAPI = false
	result, _, err := NewGetLogPatternsHandler(srv.Client(), cfg)(context.Background(), &mcp.CallToolRequest{}, GetLogPatternsArgs{
		LogPatternScopeArgs: LogPatternScopeArgs{ServiceName: "checkout", StartTimeISO: patternWindowStartISO, EndTimeISO: patternWindowEndISO, SampleSize: sampleSize},
	})
	return result, err
}

func TestGetLogPatterns_ChunkedPathSamplesWithoutTotals(t *testing.T) {
	cases := []struct {
		name       string
		sampleSize int
		wantLimit  bool
	}{
		{"short sample", 1000, false},
		{"filled sample", 2, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv, hits := chunkedLogsServer(t, http.StatusOK, "timeout calling db 1", "timeout calling db 2")
			result, err := callLogPatternsChunked(srv, c.sampleSize)
			if err != nil {
				t.Fatalf("handler: %v", err)
			}
			got := decodeToolJSON[GetLogPatternsResponse](t, result)
			if hits.Load() == 0 || got.TotalPatterns != 1 || got.Patterns[0].Template != "timeout calling db <*>" {
				t.Fatalf("hits=%d response=%+v; want the chunked upstream sampled into one template", hits.Load(), got)
			}
			if got.Window.SampleLimitReached != c.wantLimit || got.Window.TotalMatchingLines != nil || got.Window.LogsTruncated != nil {
				t.Errorf("window = %+v, want sample_limit_reached %t and no total or truncation flag", got.Window, c.wantLimit)
			}
		})
	}
}

func TestGetLogPatterns_ChunkedPathRejectsAFailedChunk(t *testing.T) {
	srv, _ := chunkedLogsServer(t, http.StatusBadGateway)
	if _, err := callLogPatternsChunked(srv, 0); err == nil {
		t.Fatal("want an error when a chunk fails, not an empty pattern list that reads as no logs")
	}
}
